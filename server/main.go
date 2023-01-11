package main

import (
	// "bufio"
	"flag"
	"fmt"
	"net"
	"os"
)

var (
	addr  string
	count int64
)

func init() {
	flag.StringVar(&addr, "addr", "0.0.0.0:9119", "listen addr")
	flag.Parse()
}

func main() {
	count = 0
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		panic(err)
	}

	for {
		conn, err := ln.Accept()
		if err != nil {
			os.Exit(1)
		}

		go handleConn(conn)
		count++
		fmt.Println("total:", count)
	}
}

func handleConn(conn net.Conn) {
	// r := bufio.NewReader(conn)
	defer conn.Close()
	for {
		// _, err := r.ReadString('\n')
		buf := make([]byte, 1024)
		_, err := conn.Read(buf)
		if err != nil {
			fmt.Println("read error", err)
		}
		_, err = conn.Write([]byte("PONG\n"))
		if err != nil {
			fmt.Println("write error", err)
			return
		}
	}
}
