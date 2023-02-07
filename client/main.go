package main

import (
	// "errors"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"net/http"
	_ "net/http/pprof"
)

var (
	server_addr               string
	conn_count                int64
	batchSize                 int64
	wg                        = sync.WaitGroup{}
	doneChan                  = make(chan bool)
	srcAddr                   string
	sendData                  bool
	jitter                    int
	interval                  int
	payloadSize               int
	payload                   []byte
	logLevel                  string
	nic                       string
	IPAddrs                   []net.Addr
	estimatedTotalConnections int64
	stateChan                 chan error
	fireChan                  chan bool
	pprof                     bool
	timeout                   int
	setBindNoPort             bool
)

type StateCounter struct {
	CumulativeAttemptingConnections  int64
	CumulativeEstablishedConnections int64
	CurrentAttemptingConnections     int64
	CurrentEstablishedConnections    int64
	Attempted                        float64
	Established                      float64
	ConnTimeout                      int64
	ReadTimeout                      int64
	WriteTimeout                     int64
	DialTimeout                      int64
	ErrorReset                       int64
	ErrorAddrInUse                   int64
	ErrorAddrNotAvail                int64
	ErrorOthers1                     int64
	ErrorOthers2                     int64
	ErrorOthers3                     int64
}

type Runner struct {
	IPIndex     int64
	States      StateCounter
	Records     []StateCounter
	curr        int
	Dialer      net.Dialer
	AddressPool []net.Addr
	OutOfPort   bool
	BatchSize   int64
}

func init() {
	rand.Seed(time.Now().UnixMilli())

	flag.StringVar(&nic, "nic", "eth0", "the network interface")
	flag.StringVar(&srcAddr, "src", "127.0.0.1", "source ip addr")
	flag.Int64Var(&conn_count, "conn", 50, "connection count")
	flag.Int64Var(&batchSize, "batch", 5, "connection batch size")
	flag.StringVar(&server_addr, "addr", "192.168.66.240:8000", "server addr")
	flag.BoolVar(&sendData, "senddata", false, "whether to send data after first time communication")
	flag.IntVar(&interval, "interval", 50, "interval between sending datas, in seconds")
	flag.IntVar(&jitter, "jitter", 1, "jitter when sending data")
	flag.IntVar(&payloadSize, "payload-size", 73, "random payload size")
	flag.StringVar(&logLevel, "log-level", "info", "log level")
	flag.BoolVar(&pprof, "pprof", false, "enable pprof")
	flag.IntVar(&timeout, "timeout", 50, "readwrite timeout seconds")
	flag.BoolVar(&setBindNoPort, "set-bind-no-port", true, "set IP_BIND_ADDRESS_NO_PORT")
	flag.Parse()

	if pprof {
		go func() {
			log.Fatalln(http.ListenAndServe("0.0.0.0:16060", nil))
		}()
	}

	var err error

	payload = make([]byte, payloadSize)
	_, err = rand.Read(payload)
	if err != nil {
		fmt.Println("Error generating payload", err)
		os.Exit(1)
	}

	var inf *net.Interface

	if inf, err = net.InterfaceByName(nic); err != nil {
		panic(fmt.Sprintf("Unable to query interface: %s", nic))
	}

	addrs, err := inf.Addrs()
	if err != nil {
		panic(fmt.Sprintf("Unable to query addrs of interface: %s", nic))
	}

	IPAddrs = make([]net.Addr, 0)
	for _, addr := range addrs {
		if !strings.HasPrefix(addr.String(), "127.0") && !strings.Contains(addr.String(), ":") {
			IPAddrs = append(IPAddrs, addr)
		}
	}

	stateChan = make(chan error)
	fireChan = make(chan bool)

	localPortRange := GetLocalPortRange()
	// exclude the default ip addrs of the NIC
	if len(IPAddrs) > 1 {
		estimatedTotalConnections = int64((len(IPAddrs) - 1) * localPortRange)
	} else {
		estimatedTotalConnections = int64(localPortRange)
	}
}

func GetLocalPortRange() int {
	cmd := exec.Command("sysctl", "-n", "net.ipv4.ip_local_port_range")
	out, _ := cmd.CombinedOutput()
	r := strings.Fields(string(out))
	start, _ := strconv.Atoi(r[0])
	end, _ := strconv.Atoi(r[1])
	return end - start
}

func (r *Runner) Connect(proto string, dst string, stateChan chan error) {
	var conn net.Conn
	var err error

	// atomic.AddInt64(&r.IPIndex, 1)
	// addr := r.AddressPool[r.IPIndex%int64(len(r.AddressPool))]
	addr := r.AddressPool[rand.Intn(len(r.AddressPool))]
	src := &net.TCPAddr{
		IP: net.ParseIP(strings.Split(addr.String(), "/")[0]),
	}

	dialer := net.Dialer{
		LocalAddr: src,
		// Timeout:   120 * time.Second,
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
				if setBindNoPort {
					unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_BIND_ADDRESS_NO_PORT, 1)
				}
			})
			return e
		},
	}

	atomic.AddInt64(&r.States.CurrentAttemptingConnections, 1)
	atomic.AddInt64(&r.States.CumulativeAttemptingConnections, 1)
	conn, err = dialer.Dial(proto, dst)

	if err != nil {
		atomic.AddInt64(&r.States.CurrentAttemptingConnections, -1)
		if err == os.ErrDeadlineExceeded {
			atomic.AddInt64(&r.States.ConnTimeout, 1)
		}
		stateChan <- err
		return
	}

	defer func() {
		conn.Close()
	}()

	if tcp, ok := conn.(*net.TCPConn); ok {
		tcp.SetKeepAlive(false)
		tcp.SetLinger(0)
	}

	countConnected := false

	for {
		recvBuf := make([]byte, 1024)

		conn.SetDeadline(time.Now().Add(time.Duration(timeout) * time.Second))
		_, err = conn.Write(payload)
		if err != nil {
			if err == os.ErrDeadlineExceeded {
				atomic.AddInt64(&r.States.WriteTimeout, 1)
			}
			atomic.AddInt64(&r.States.CurrentEstablishedConnections, -1)
			stateChan <- err
			return
		}

		conn.SetDeadline(time.Now().Add(time.Duration(timeout) * time.Second))
		_, err = conn.Read(recvBuf[:])
		if !countConnected {
			atomic.AddInt64(&r.States.CumulativeEstablishedConnections, 1)
			atomic.AddInt64(&r.States.CurrentEstablishedConnections, 1)
			atomic.AddInt64(&r.States.CurrentAttemptingConnections, -1)
			countConnected = true
		}

		if err != nil {
			if err == os.ErrDeadlineExceeded {
				atomic.AddInt64(&r.States.ReadTimeout, 1)
			}
			atomic.AddInt64(&r.States.CurrentEstablishedConnections, -1)
			stateChan <- err
			return
		}

		if !sendData {
			select {}
		}

		time.Sleep(time.Duration(interval+rand.Intn(jitter)) * time.Second)
	}
}

func (r *Runner) fireConnection(firechan chan bool) {
	tick := time.Tick(time.Second)

	for {
		select {
		case _ = <-tick:
			var batch int64
			totalConn := estimatedTotalConnections - 500
			currentConn := r.States.CurrentAttemptingConnections + r.States.CurrentEstablishedConnections
			remaing := totalConn - currentConn

			if remaing >= r.BatchSize {
				batch = r.BatchSize
			} else {
				batch = remaing
			}

			if currentConn < totalConn {
				for i := int64(0); i <= batch; i++ {
					go r.Connect("tcp", server_addr, stateChan)
				}
				r.OutOfPort = false
			} else {
				r.OutOfPort = true
				continue
			}
		}
	}
}

func (r *Runner) HandleErrors(stateChan chan error) {
	for {
		select {
		case err := <-stateChan:
			ne, ok := err.(*net.OpError)
			if ok {
				switch {
				case errors.Is(ne, syscall.ECONNRESET):
					atomic.AddInt64(&r.States.ErrorReset, 1)
				case errors.Is(ne, syscall.EADDRINUSE):
					atomic.AddInt64(&r.States.ErrorAddrInUse, 1)
				case errors.Is(ne, syscall.EADDRNOTAVAIL):
					atomic.AddInt64(&r.States.ErrorAddrNotAvail, 1)
				case ne.Timeout():
					if ne.Op == "read" {
						atomic.AddInt64(&r.States.ReadTimeout, 1)
					} else if ne.Op == "write" {
						atomic.AddInt64(&r.States.WriteTimeout, 1)
					} else if ne.Op == "dial" {
						atomic.AddInt64(&r.States.DialTimeout, 1)
					} else {
						atomic.AddInt64(&r.States.ErrorOthers1, 1)
					}
				default:
					atomic.AddInt64(&r.States.ErrorOthers2, 1)
				}
			} else {
				atomic.AddInt64(&r.States.ErrorOthers3, 1)
			}

		}
	}
}

func (r *Runner) ReportStates() {
	prev := r.Records[r.curr]
	now := r.States
	r.curr ^= r.curr
	r.Records[r.curr] = now

	// estabPerItv := r.Records[r.curr].CurrentEstablishedConnections - prev.CurrentEstablishedConnections
	estabPerItv := now.CurrentEstablishedConnections - prev.CurrentEstablishedConnections

	fmt.Println("================", time.Now(), r.OutOfPort)
	fmt.Printf("estimatedTotalConnections: %d\n", estimatedTotalConnections)
	fmt.Printf("CumulativeAttemptingConnections: %d\n", r.States.CumulativeAttemptingConnections)
	fmt.Printf("CumulativeEstablishedConnections: %d\n", r.States.CumulativeEstablishedConnections)
	fmt.Printf("CurrentAttemptingConnections: %d\n", r.States.CurrentAttemptingConnections)
	fmt.Printf("CurrentEstablishedConnections: %d\n", r.States.CurrentEstablishedConnections)
	fmt.Printf("EstablishedPerSecond: %d\n", estabPerItv)
	fmt.Printf("ConnTimeout: %d\n", r.States.ConnTimeout)
	fmt.Printf("WriteTimeout: %d\n", r.States.WriteTimeout)
	fmt.Printf("DialTimeout: %d\n", r.States.DialTimeout)
	fmt.Printf("ReadTimeout: %d\n", r.States.ReadTimeout)
	fmt.Printf("ErrorReset: %d\n", r.States.ErrorReset)
	fmt.Printf("ErrorAddrInUse: %d\n", r.States.ErrorAddrInUse)
	fmt.Printf("ErrorAddrNotAvail: %d\n", r.States.ErrorAddrNotAvail)
	fmt.Printf("ErrorAddrOthers1: %d\n", r.States.ErrorOthers1)
	fmt.Printf("ErrorAddrOthers2: %d\n", r.States.ErrorOthers2)
	fmt.Printf("ErrorAddrOthers3: %d\n", r.States.ErrorOthers3)

}

func main() {
	if jitter <= 0 {
		jitter = 1
	}

	// Start Runner and count
	runner := Runner{
		OutOfPort:   false,
		States:      StateCounter{},
		Records:     make([]StateCounter, 2),
		AddressPool: IPAddrs,
		BatchSize:   batchSize,
	}

	ticker := time.NewTicker(time.Second)

	go runner.fireConnection(fireChan)
	go runner.HandleErrors(stateChan)

	for {
		select {
		// case fire := <-fireChan:
		// 	if fire {
		// 		go runner.Connect("tcp", server_addr, stateChan)
		// 	}
		case <-ticker.C:
			runner.ReportStates()
		}
	}

}
