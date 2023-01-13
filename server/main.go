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
)

func init() {
	flag.StringVar(&addr, "addr", "0.0.0.0:9119", "listen addr")
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
	defer conn.Close()
	select {}
}
