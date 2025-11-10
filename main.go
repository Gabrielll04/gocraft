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
)

type ServerboundPacket interface{}

// packets received from client
type Handshake struct {
	ProtocolVersion int32
	Address         string
	Port            uint16
	NextState       int32
}

type Request struct{}
type Ping struct{ Payload int64 }




type ClientboundPacket interface{}

// packets to sent to the client
type Pong struct{ Payload int64 }

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
		return "", nil
	}

	return string(buf), nil
}

func ReadPacket(r io.Reader, state PacketState) (ServerboundPacket, error) {
	_, err := ReadVarInt(r)
	if err != nil {
		return nil, err
	}

	packetID, err := ReadVarInt(r)
	if err != nil {
		return nil, err
	}

	switch state {
	case Handshaking:
		if packetID == 0x00 {
			ProtocolVersion, err := ReadVarInt(r)
			if err != nil {
				return nil, err
			}

			Address, err := ReadString(r)
			if err != nil {
				return nil, err
			}

			var port uint16
			if err := binary.Read(r, binary.BigEndian, &port); err != nil {
				return nil, err
			}

			NextState, err := ReadVarInt(r)
			if err != nil {
				return nil, err
			}

			return Handshake{
				ProtocolVersion: ProtocolVersion,
				Address: 	     Address,
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
			if err := binary.Read(r, binary.BigEndian, &payload); err != nil {
				return nil, err
			}
			return Ping{Payload: payload}, nil
		}
	}

	return nil, errors.New("unknown packet id or state")
}

func handlePacket(packet ServerboundPacket) (ClientboundPacket, bool) {
	switch p := packet.(type) {
	case Handshake:
		if p.NextState == 1 { // gonna work on this later
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

	length := int32(len(packetData) + 1)
	WriteVarInt(&buf, length)
	WriteVarInt(&buf, packetId)
	buf.Write(packetData)

	fmt.Println("writing ", packetId, " packet fields")

	return buf.Bytes()
}

func WritePacket(packet ClientboundPacket) ([]byte, error) {
	switch p := packet.(type) {
	case Response:
		encoded, err := json.Marshal(p)
		if err != nil {
			return nil, err
		}

		var payload bytes.Buffer
		if err := WriteVarInt(&payload, int32(len(encoded))); err != nil {
			return nil, err
		}
		payload.Write(encoded)

		return writePacketFields(0x00, payload.Bytes()), nil

	case Pong:
		var payload bytes.Buffer

		binary.Write(&payload, binary.BigEndian, p.Payload)
		return writePacketFields(0x01, payload.Bytes()), nil

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
			fmt.Println("An error occurred while reading the packet:", err)
			return
		}

		fmt.Println("Packet Received:", packet)

		switch p := packet.(type) {
		case Handshake:
			if p.NextState == 1 {
				state = Status
			} else {
				fmt.Println("Unsupported next state:", p.NextState)
				return
			}

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
