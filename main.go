package gocraft

import (
	"encoding/binary"
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

type Handshake struct {
	ProtocolVersion int32
	Address         string
	Port            uint16
	NextState       int32
}

type Request struct{}
type Ping struct{ Payload int64 }

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
			if err != nil {
				return err
			}
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

func handleConnection(conn net.Conn) {
	defer conn.Close()

	fmt.Println("New connection:", conn.RemoteAddr())

	for {
		packetLength, err := ReadVarInt(conn)
		if err != nil {
			if err == io.EOF {
				fmt.Println("Client disconnected.")
				return
			}
			fmt.Println("Erro ao ler comprimento:", err)
			return
		}

		packetData := make([]byte, packetLength)
		_, err = io.ReadFull(conn, packetData)
		if err != nil {
			fmt.Println("Erro ao ler pacote:", err)
			return
		}

		// 3. Agora você pode processar o conteúdo (ex: ler o Packet ID)
		fmt.Printf("Pacote recebido (%d bytes): %x\n", packetLength, packetData)

		// Exemplo: ler o primeiro VarInt dentro do pacote (Packet ID)
		// pode usar um bytes.Reader para isso:
		// packetReader := bytes.NewReader(packetData)
		// packetID, _ := ReadVarInt(packetReader)
		// fmt.Println("Packet ID:", packetID)
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
