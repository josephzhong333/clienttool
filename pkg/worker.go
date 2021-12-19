package worker

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	RetryTimes   = 3
	Timeout      = 50
	RequestCount = 100

	QueryIPFailed = -2
	Completed     = 0
	Abort         = -5
)

type (
	Config struct {
		Name     string
		LogLevel string
	}

	Worker struct {
		*zap.SugaredLogger
		IPs []string
		Cfg Config
		Ac  ActiveConnection
	}

	ActiveConnection struct {
		ActiveConnectionCount []int
		Locker                sync.Mutex
	}
)

func NewWorker(config Config) *Worker {
	l, e := zap.NewDevelopment()
	defer l.Sync()
	if e != nil {
		panic(e)
	}
	switch config.LogLevel {
	case "info":
		l = l.WithOptions(zap.IncreaseLevel(zapcore.InfoLevel))
	case "error":
		l = l.WithOptions(zap.IncreaseLevel(zapcore.ErrorLevel))
	case "debug":
		l = l.WithOptions(zap.IncreaseLevel(zapcore.DebugLevel))
	default:
		l = l.WithOptions(zap.IncreaseLevel(zapcore.InfoLevel))
	}
	w := &Worker{
		IPs: []string{},
		Cfg: config,
	}
	w.SugaredLogger = l.Sugar()
	return w
}

// Run the process is composed of 6 phases:
// 1, get IPs of target domain name
// 2, create corresponding goroutine and channel for each IP
// 3, start a load balance goroutine to distribute request to each IP processing channel
// 4, each goroutine listen to itself channel and send request to it's IP, if timeout or status code >500,
// send the failed request id back to request channel
// 5, load balance channel distribute the failed request again
// 6, if all the request completed(completed count>=100), the entire process completed
func (w *Worker) Run() int {
	start := time.Now().UnixNano() / 1e6
	ips, e := w.GetIPs(w.Cfg.Name)
	if e != nil {
		w.Errorf("after retry, still failed to get the IPs of target domain name:\"%s\", Please check the input domain name or try again later.", w.Cfg.Name)
		return QueryIPFailed
	} else {
		for _, v := range ips {
			w.IPs = append(w.IPs, v)
		}
		w.Infof("found ip addresses:%v", w.IPs)
	}
	w.Infof("querying ip consumed %d ms", time.Now().UnixNano()/1e6-start)

	startTime := time.Now().UnixNano() / 1e6

	IPCount := len(ips)
	reqChan := make(chan int, RequestCount)
	processingChan := make([]chan int, IPCount)
	finishChan := make(chan int, RequestCount)
	ac := ActiveConnection{
		ActiveConnectionCount: []int{},
	}
	acc := ac.ActiveConnectionCount
	for index := 0; index < IPCount; index++ {
		acc = append(acc, 0)
	}
	ac.ActiveConnectionCount = acc
	w.Ac = ac
	for i, ip := range ips {
		processingChan[i] = make(chan int, RequestCount)
		go w.StartGoroutine(i, ip, processingChan[i], finishChan, reqChan)
	}

	w.Debugf("init %d goroutine and channel consumed time:%dms", IPCount, time.Now().UnixNano()/1e6-startTime)

	// load balance scheduler goroutine
	// the least active connections first algorithm
	go func() {
		for i := 1; i <= RequestCount; i++ {
			reqChan <- i
		}
		for {
			select {
			case c := <-reqChan:
				w.Ac.Locker.Lock()
				acCopy := make([]int, IPCount)
				w.Debugf("in lb goroutine, w.Ac.ActiveConnectionCount for each IP:%v", w.Ac.ActiveConnectionCount)
				copy(acCopy, w.Ac.ActiveConnectionCount)
				w.Ac.Locker.Unlock()

				min := RequestCount
				var minIndex []int
				for i, v := range acCopy {
					w.Debugf("goroutine:%d has %d active connections", i, v)
					if v < min {
						min = v
						minIndex = []int{}
						minIndex = append(minIndex, i)
					} else if v == min {
						minIndex = append(minIndex, i)
					}
				}

				minIndexLength := len(minIndex)
				if minIndexLength == 1 {
					w.Debugf("the least active connections is number %v goroutine, %d active connections remaining", minIndex, min)
					processingChan[minIndex[0]] <- c
				} else if minIndexLength == 0 {
					w.Debugf("no active connection count collected yet, simply distribute request by mod operation")
					processingChan[c%IPCount] <- c
				} else if minIndexLength > 0 {
					w.Debugf("the least active connections is number %v goroutine, they each have %d active connections remaining", minIndex, min)
					processingChan[minIndex[c%len(minIndex)]] <- c
				}
			}
		}
	}()

	resultTag := Abort
	timeoutTimer := time.NewTimer(Timeout * time.Second)
	count := 0
	for {
		select {
		case n := <-finishChan:
			w.Debugf("finished request:%d", n)
			count++
			if count >= RequestCount {
				w.Debugf("total finished %d requests", count)
				resultTag = Completed
				goto EndInfo
			}
		case <-timeoutTimer.C:
			w.Errorf("after %d seconds, requests still not completed, abort", Timeout)
			resultTag = Abort
			goto EndInfo
		}
	}

EndInfo:
	if resultTag == Completed {
		end := time.Now().UnixNano() / 1e6
		w.Infof("completed %d requests, totally consumed %d ms", RequestCount, end-startTime)
	} else {
		w.Errorf("please check target domain is accessible or try again later")
	}
	return resultTag
}

// GetIPs
// Get IPs of target domain by using golang library net.LookupHost
func (w *Worker) GetIPs(name string) ([]string, error) {
	var ips []string
	var e error
	for i := 0; i < RetryTimes; i++ {
		ips, e = net.LookupHost(name)
		if e == nil && len(ips) > 0 {
			break
		}
	}
	w.Debugf("IPs including IPv6: %v", ips)
	if len(ips) == 0 {
		return nil, errors.New("ip not found")
	}
	var ips2 []string
	for _, v := range ips {
		if nil != net.ParseIP(v).To4() {
			ips2 = append(ips2, v)
		}
	}
	w.Debugf("query domain name:%s, got result: IPs exclude IPv6:%v", name, ips2)
	if len(ips2) == 0 {
		return nil, errors.New("ip not found")
	} else {
		return ips2, nil
	}
}

// StartGoroutine start a dedicated goroutine for the backend IP, to handle the request distributed to this IP
func (w *Worker) StartGoroutine(index int, ip string, receiveCh chan int, finishCh chan int, requestCh chan int) {
	c := &http.Client{
		Transport: &http.Transport{
			MaxIdleConnsPerHost: 10,
			MaxConnsPerHost:     100,
			DialContext: (&net.Dialer{
				Timeout:   3 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
		Timeout: time.Duration(3) * time.Second,
	}
	for {
		select {
		case n := <-receiveCh:
			w.Debugf("IP:%s, received request:%d", ip, n)
			go func(x int) {
				w.Ac.Locker.Lock()
				w.Debugf("w.Ac.ActiveConnectionCount[%d] is %d", index, w.Ac.ActiveConnectionCount[index])
				w.Ac.ActiveConnectionCount[index]++
				w.Ac.Locker.Unlock()
				req, e := http.NewRequest("Get", fmt.Sprintf("http://%s", ip), nil)
				req.Host = w.Cfg.Name
				if e != nil {
					w.Errorf("generate http request failed:%d", x)
				}
				resp, err := c.Do(req)
				if err != nil {
					w.Errorf("%s:get response failed due to %s, retry request:%d", ip, err.Error(), x)
					requestCh <- x
				} else {
					if resp.StatusCode >= 500 {
						w.Errorf("request:%d return status code >500,retry", x)
						requestCh <- x
					}
					defer resp.Body.Close()
					finishCh <- x
				}
				w.Ac.Locker.Lock()
				w.Ac.ActiveConnectionCount[index]--
				w.Ac.Locker.Unlock()
			}(n)
		}
	}
}
