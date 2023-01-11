package main

import (
	// "bufio"
	"flag"
	"fmt"
	"math/rand"
	"net"
	"sync"
	"time"
)

var (
	server_addr string
	conn_count  int64
	batchSize   int64
	wg          = sync.WaitGroup{}
	doneChan    = make(chan bool)
	srcAddr     string
)

func init() {
	flag.StringVar(&srcAddr, "src", "127.0.0.1", "source ip addr")
	flag.Int64Var(&conn_count, "conn", 50, "connection count")
	flag.Int64Var(&batchSize, "batch", 5, "connection batch size")
	flag.StringVar(&server_addr, "addr", "192.168.66.240:8000", "server addr")
	flag.Parse()
}

func main() {

	ip := net.ParseIP(srcAddr)

	src := &net.TCPAddr{
		IP: ip,
	}

	start := time.Now()
	for i := int64(0); i < conn_count; {
		for j := int64(0); j < batchSize; j++ {
			wg.Add(1)
			go connect(&wg, doneChan, src, server_addr)
			i++
		}
		if conn_count-i < batchSize {
			batchSize = conn_count - i
		}
		fmt.Println("Wait", i)
		wg.Wait()
	}
	fmt.Println("All set", time.Since(start))
	select {}
}

func connect(wg *sync.WaitGroup, done chan bool, src *net.TCPAddr, dst string) {
	var conn net.Conn
	var err error

	dailer := net.Dialer{
		LocalAddr: src,
		Timeout:   80 * time.Second,
		KeepAlive: 90 * time.Second,
	}
	for {
		conn, err = dailer.Dial("tcp", dst)
		if err == nil {
			break
		}
		fmt.Println("dail error:", err)
		time.Sleep(time.Duration(200+rand.Intn(100)) * time.Millisecond)
	}

	// defer wg.Done()
	defer conn.Close()
	wg.Done()

	for {
		recvBuf := make([]byte, 1024)

		_, err = conn.Write([]byte("PING\n"))
		if err != nil {
			fmt.Println("write error", err)
		}

		_, err = conn.Read(recvBuf[:])

		if err != nil {
			fmt.Println("read error", err)
		}

		_, err = conn.Read(recvBuf[:])
		time.Sleep(time.Duration(1800+rand.Intn(1800)) * time.Second)
	}
}
