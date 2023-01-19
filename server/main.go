package main

import (
	// "bufio"
	"context"
	"flag"
	"fmt"
	// "io"
	// "math/rand"
	"net"
	"os"
	// "time"
)

var (
	addr  string
	count int64
	echo  bool
)

func init() {
	flag.StringVar(&addr, "addr", "0.0.0.0:9119", "listen addr")
	flag.BoolVar(&echo, "echo", true, "is a echo server?")
	flag.Parse()
}

func main() {
	count = 0
	lc := net.ListenConfig{
		KeepAlive: -1,
	}
	ln, err := lc.Listen(context.Background(), "tcp", addr)
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
	var err error

	defer conn.Close()

	if echo {
		for {
			// _, err := r.ReadString('\n')
			buf := make([]byte, 1024)
			_, err = conn.Read(buf)
			if err != nil {
				fmt.Println("read error", err)
				return
			}
			_, err = conn.Write([]byte("PONG\n"))
			if err != nil {
				fmt.Println("write error", err)
				return
			}
		}
	}

	for {
		buf := make([]byte, 1024)
		_, err = conn.Read(buf)
		if err != nil {
			fmt.Println("read error", err)
			return
		}
	}
}
