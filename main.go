package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
)

type PacketState int

const (
	Handshaking PacketState = iota
	Status
	Login
	Play
)

type ServerboundPacket interface{}

type Handshake struct {
	ProtocolVersion int32
	Address         string
	Port            uint16
	NextState       int32
}

type Request struct{}

type Ping struct{ Payload int64 }

type LoginStart struct {
	Name string
}

type ClientboundPacket interface{}

type Pong struct{ Payload int64 }

type LoginSuccess struct {
	UUID     string
	Username string
}

type Response struct {
	Version     Version     `json:"version"`
	Players     Players     `json:"players"`
	Description Description `json:"description"`
}

type Version struct {
	Name     string `json:"name"`
	Protocol int    `json:"protocol"`
}

type Players struct {
	Max    uint32   `json:"max"`
	Online uint32   `json:"online"`
	Sample []Player `json:"sample"`
}

type Player struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

type Description struct {
	Text string `json:"text"`
}

func ReadVarInt(r io.Reader) (int32, error) {
	var numRead int
	var result int32
	var buf [1]byte

	for {
		if numRead > 5 {
			return 0, errors.New("VarInt too big")
		}

		_, err := r.Read(buf[:])
		if err != nil {
			return 0, err
		}

		byteVal := buf[0]
		value := int32(byteVal & 0x7F)
		result |= value << (7 * numRead)
		numRead++

		if (byteVal & 0x80) == 0 {
			break
		}
	}
	return result, nil
}

func WriteVarInt(w io.Writer, value int32) error {
	for {
		if (value & ^0x7F) == 0 {
			_, err := w.Write([]byte{byte(value)})
			return err
		}

		b := byte(value&0x7F) | 0x80
		if _, err := w.Write([]byte{b}); err != nil {
			return err
		}
		value >>= 7
	}
}

func ReadString(r io.Reader) (string, error) {
	length, err := ReadVarInt(r)
	if err != nil {
		return "", err
	}

	buf := make([]byte, length)
	_, err = io.ReadFull(r, buf)
	if err != nil {
		return "", err
	}

	return string(buf), nil
}

func ReadPacket(r io.Reader, state PacketState) (ServerboundPacket, error) {
	length, err := ReadVarInt(r)
	if err != nil {
		return nil, err
	}

	packetData := make([]byte, length)
	_, err = io.ReadFull(r, packetData)
	if err != nil {
		return nil, err
	}

	packetReader := bytes.NewReader(packetData)

	packetID, err := ReadVarInt(packetReader)
	if err != nil {
		return nil, err
	}

	switch state {
	case Handshaking:
		if packetID == 0x00 {
			ProtocolVersion, err := ReadVarInt(packetReader)
			if err != nil {
				return nil, err
			}

			Address, err := ReadString(packetReader)
			if err != nil {
				return nil, err
			}

			var port uint16
			if err := binary.Read(packetReader, binary.BigEndian, &port); err != nil {
				return nil, err
			}

			NextState, err := ReadVarInt(packetReader)
			if err != nil {
				return nil, err
			}

			return Handshake{
				ProtocolVersion: ProtocolVersion,
				Address:         Address,
				Port:            port,
				NextState:       NextState,
			}, nil
		}
	case Status:
		switch packetID {
		case 0x00:
			return Request{}, nil
		case 0x01:
			var payload int64
			if err := binary.Read(packetReader, binary.BigEndian, &payload); err != nil {
				return nil, err
			}
			return Ping{Payload: payload}, nil
		}
	case Login:
		switch packetID {
		case 0x00:
			name, err := ReadString(packetReader)
			if err != nil {
				return nil, err
			}
			return LoginStart{Name: name}, nil
		}
	}

	return nil, errors.New("unknown packet id or state")
}

func handlePacket(packet ServerboundPacket) (ClientboundPacket, bool) {
	switch p := packet.(type) {
	case Handshake:
		if p.NextState == 1 {
			return nil, false
		}
		return nil, false
	case Request:
		return resp, true
	case Ping:
		return Pong(p), true
	default:
		return nil, false
	}
}

var resp = Response{
	Version: Version{
		Name:     "1.19",
		Protocol: 759,
	},
	Players: Players{
		Max:    100,
		Online: 0,
		Sample: []Player{},
	},
	Description: Description{
		Text: "Servidor GoCraft :)",
	},
}

func writePacketFields(packetId int32, packetData []byte) []byte {
	var buf bytes.Buffer
	var packet bytes.Buffer

	WriteVarInt(&packet, packetId)
	packet.Write(packetData)

	WriteVarInt(&buf, int32(packet.Len()))
	buf.Write(packet.Bytes())

	fmt.Println("Writing packet", packetId, "with length", packet.Len())
	return buf.Bytes()
}

func WriteString(w io.Writer, s string) error {
	if err := WriteVarInt(w, int32(len(s))); err != nil {
		return err
	}
	_, err := w.Write([]byte(s))
	return err
}

func writeLoginSuccessPacketFields(uuid, username string) []byte {
	var payload bytes.Buffer
	WriteString(&payload, uuid)
	WriteString(&payload, username)
	WriteVarInt(&payload, 0)
	return payload.Bytes()
}

func WritePacket(packet ClientboundPacket) ([]byte, error) {
	switch p := packet.(type) {
	case Response:
		encoded, err := json.Marshal(p)
		if err != nil {
			return nil, err
		}

		var payload bytes.Buffer
		WriteString(&payload, string(encoded))
		return writePacketFields(0x00, payload.Bytes()), nil

	case Pong:
		var payload bytes.Buffer
		binary.Write(&payload, binary.BigEndian, p.Payload)
		return writePacketFields(0x01, payload.Bytes()), nil

	case LoginSuccess:
		payload := writeLoginSuccessPacketFields("00000000-0000-0000-0000-000000000000", p.Username)
		return writePacketFields(0x02, payload), nil

	default:
		return nil, fmt.Errorf("unknown packet type: %T", p)
	}
}

func handleConnection(conn net.Conn) {
	defer conn.Close()
	fmt.Println("New connection:", conn.RemoteAddr())

	state := Handshaking

	for {
		packet, err := ReadPacket(conn, state)
		if err != nil {
			if err != io.EOF {
				fmt.Println("An error occurred while reading the packet:", err)
			} else {
				fmt.Println("Connection closed by client")
			}
			return
		}

		fmt.Println("Packet Received:", packet)

		switch p := packet.(type) {
		case Handshake:
			switch p.NextState {
			case 1:
				state = Status
			case 2:
				state = Login
			default:
				fmt.Println("Unsupported next state:", p.NextState)
				return
			}

		case LoginStart:
			fmt.Println("Player logging in:", p.Name)

			loginSuccess := LoginSuccess{
				UUID:     "00000000-0000-0000-0000-000000000000",
				Username: p.Name,
			}

			data, err := WritePacket(loginSuccess)
			if err != nil {
				fmt.Println("Error creating login success packet:", err)
				return
			}

			_, err = conn.Write(data)
			if err != nil {
				fmt.Println("Error sending login success packet:", err)
				return
			}

			fmt.Println("Sent LoginSuccess to", p.Name)
			state = Play

			fmt.Println("Login completed, but Play state not fully implemented yet")
			return

		default:
			resp, ok := handlePacket(packet)
			if !ok {
				continue
			}

			data, err := WritePacket(resp)
			if err != nil {
				fmt.Println("Error while writing packet:", err)
				return
			}

			_, err = conn.Write(data)
			if err != nil {
				fmt.Println("Error sending response:", err)
				return
			}

			fmt.Println("Sent response:", data)
		}
	}
}

func main() {
	ln, err := net.Listen("tcp", ":25565")
	if err != nil {
		fmt.Println("Erro ao criar servidor:", err)
		os.Exit(1)
	}

	fmt.Println("Servidor iniciado na porta 25565")

	for {
		conn, err := ln.Accept()
		if err != nil {
			fmt.Println("Erro ao aceitar conexão:", err)
			continue
		}

		go handleConnection(conn)
	}
}