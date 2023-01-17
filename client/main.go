package main

import (
	"flag"
	"fmt"
	"math/rand"
	"net"
	"os"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

var (
	server_addr string
	conn_count  int64
	batchSize   int64
	wg          = sync.WaitGroup{}
	doneChan    = make(chan bool)
	srcAddr     string
	sendData    bool
	jitter      int
	interval    int
	payloadSize int
	payload     []byte
	logLevel    string
)

func init() {
	flag.StringVar(&srcAddr, "src", "127.0.0.1", "source ip addr")
	flag.Int64Var(&conn_count, "conn", 50, "connection count")
	flag.Int64Var(&batchSize, "batch", 5, "connection batch size")
	flag.StringVar(&server_addr, "addr", "192.168.66.240:8000", "server addr")
	flag.BoolVar(&sendData, "senddata", false, "whether to send data after first time communication")
	flag.IntVar(&interval, "interval", 50, "interval between sending datas, in seconds")
	flag.IntVar(&jitter, "jitter", 1, "jitter when sending data")
	flag.IntVar(&payloadSize, "payload-size", 73, "random payload size")
	flag.StringVar(&logLevel, "log-level", "info", "log level")
	flag.Parse()

	payload = make([]byte, payloadSize)
	_, err := rand.Read(payload)
	if err != nil {
		fmt.Println("Error generating payload", err)
		os.Exit(1)
	}

}

func main() {
	if jitter <= 0 {
		jitter = 1
	}

	ip := net.ParseIP(srcAddr)

	src := &net.TCPAddr{
		IP: ip,
	}

	dialer := net.Dialer{
		LocalAddr: src,
		Timeout:   120 * time.Second,
		KeepAlive: -1,
		DualStack: true,
		Control: func(network, address string, c syscall.RawConn) error {
			e := c.Control(func(fd uintptr) {
				// SO_REUSEPORT
				unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
				// SO_REUSEADDR
				unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)

				// https://blog.cloudflare.com/how-to-stop-running-out-of-ephemeral-ports-and-start-to-love-long-lived-connections/
				// https://kernelnewbies.org/Linux_4.2#Networking
				unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_BIND_ADDRESS_NO_PORT, 1)
			})
			return e
		},
	}

	start := time.Now()
	for i := int64(0); i < conn_count; {
		for j := int64(0); j < batchSize; j++ {
			wg.Add(1)
			go connect(&wg, dialer, src, server_addr)
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

func connect(wg *sync.WaitGroup, dialer net.Dialer, src *net.TCPAddr, dst string) {
	var conn net.Conn
	var err error

	for {
		conn, err = dialer.Dial("tcp", dst)
		if err == nil {
			break
		}
		fmt.Println("dail error:", err)
		time.Sleep(time.Duration(200+rand.Intn(100)) * time.Millisecond)
	}

	if tcp, ok := conn.(*net.TCPConn); ok {
		tcp.SetKeepAlive(false)
	}

	// defer wg.Done()
	defer conn.Close()
	wg.Done()

	for {
		recvBuf := make([]byte, 1024)

		_, err = conn.Write(payload)
		if err != nil {
			fmt.Println("write error", err)
		}

		_, err = conn.Read(recvBuf[:])

		if err != nil {
			fmt.Println("read error", err)
		}

		if !sendData {
			select {}
		}

		time.Sleep(time.Duration(interval+rand.Intn(jitter)) * time.Second)
	}
}
