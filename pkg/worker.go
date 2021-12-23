package worker

import (
    "errors"
    "fmt"
    "math"
    "net"
    "net/http"
    "os/exec"
    "runtime"
    "sync"
    "time"

    "go.uber.org/zap"
    "go.uber.org/zap/zapcore"
)

const (
    RetryTimes   = 3
    Timeout      = 500
    RequestCount = 100

    QueryIPFailed    = -2
    Completed        = 0
    Abort            = -5
    QueryDNSInterval = 200
)

type (
    Config struct {
        Name     string
        LogLevel string
    }

    Worker struct {
        *zap.SugaredLogger
        IPs            []string
        Cfg            Config
        Ac             ActiveConnection
        reqChan        chan int
        processingChan []chan int
        finishChan     chan int
        LatestIPIndex  []int //the array consists of index of IPs in the latest DNS query result
        QueryDNSFail   chan int
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
// 1, start a dedicated goroutine to continuously query DNS
// 2, once got a new IP, create a corresponding goroutine and channel for that IP.
// Meanwhile, maintain an index of which IP-goroutines will be used according to the latest DNS query result
// 3, start a load balance goroutine to distribute request to each IP processing channel
// 4, each goroutine listen to itself channel and send http request, if timeout or status code >500, retry immediately in-place
// if retry still failed, send the failed request id back to request channel
// 5, load balance channel distribute the failed request again
// 6, if all the request completed(completed count>=100), the entire process completed.
// Or the entire process last a Timeout duration, Abort the entire process
func (w *Worker) Run() int {
    start := time.Now().UnixNano() / 1e6
    w.reqChan = make(chan int, RequestCount)
    w.processingChan = make([]chan int, 0, 10)
    w.finishChan = make(chan int, RequestCount)
    w.QueryDNSFail = make(chan int, 10)
    ac := ActiveConnection{
        ActiveConnectionCount: []int{},
    }
    w.Ac = ac
    w.LatestIPIndex = []int{}

    // dedicated goroutine to continuously query dns and operate the IP corresponding goroutine
    go func() {
        for {
            ips, e := w.GetIPs(w.Cfg.Name)
            if e != nil {
                w.Errorf("failed to get the IPs of target domain name:\"%s\", Please check the input domain name or try again later.", w.Cfg.Name)
                w.QueryDNSFail <- 1
            } else {
                w.Debugf("found ip addresses:%v", ips)
                w.LatestIPIndex = w.LatestIPIndex[:0]
                for _, v := range ips {
                    match := false
                    for index, x := range w.IPs {
                        if x == v {
                            match = true
                            w.Debugf("IP:%s already exists", v)
                            w.LatestIPIndex = append(w.LatestIPIndex, index)
                        }
                    }
                    if match == false {
                        w.Debugf("IP:%s goroutine not exists, create goroutine and channel for this IP", v)
                        w.IPs = append(w.IPs, v)
                        processingChan := make(chan int)
                        w.processingChan = append(w.processingChan, processingChan)
                        go w.StartGoroutine(len(w.IPs)-1, v, processingChan, w.finishChan, w.reqChan)
                        w.Ac.Locker.Lock()
                        acc := w.Ac.ActiveConnectionCount
                        acc = append(acc, 0)
                        w.Ac.ActiveConnectionCount = acc
                        w.Ac.Locker.Unlock()
                        w.LatestIPIndex = append(w.LatestIPIndex, len(w.IPs)-1)
                        w.Debugf("w is %v", w)
                        w.Debugf("latest ip index in use is %v", w.LatestIPIndex)
                    }
                }
            }
            time.Sleep(QueryDNSInterval * time.Millisecond)
            w.FlushDNSCache()
        }
    }()

    // load balance scheduler goroutine
    // the least active connections first algorithm
    go func() {
        for i := 1; i <= RequestCount; i++ {
            w.reqChan <- i
        }
        for len(w.Ac.ActiveConnectionCount) == 0 {

        }
        for {
            select {
            case c := <-w.reqChan:
                w.Ac.Locker.Lock()
                IPCount := len(w.Ac.ActiveConnectionCount)
                acCopy := make([]int, IPCount)
                w.Debugf("in lb goroutine, w.Ac.ActiveConnectionCount for each IP:%v", w.Ac.ActiveConnectionCount)
                copy(acCopy, w.Ac.ActiveConnectionCount)
                w.Ac.Locker.Unlock()

                min := math.MaxInt32
                var minIndex []int
                for i, v := range acCopy {
                    //w.Debugf("in lb goroutine:%d has %d active connections", i, v)
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
                    w.Debugf("in lb goroutine:the least active count is number %v goroutine, %d active connections remaining", minIndex, min)
                    w.processingChan[minIndex[0]] <- c
                } else if minIndexLength == 0 {
                    w.Debugf("in lb goroutine:no active count collected yet, simply distribute request by mod operation")
                    w.processingChan[c%IPCount] <- c
                } else if minIndexLength > 1 {
                    w.Debugf("in lb goroutine:the least active count is number %v goroutine, they each have %d active connections remaining", minIndex, min)
                    w.processingChan[minIndex[c%len(minIndex)]] <- c
                }
            }
        }
    }()

    resultTag := Abort
    timeoutTimer := time.NewTimer(Timeout * time.Second)
    count := 0
    for {
        select {
        case n := <-w.finishChan:
            w.Debugf("finished request:%d", n)
            count++
            if count >= RequestCount {
                w.Debugf("total finished %d requests", count)
                resultTag = Completed
                goto EndInfo
            }
        case <-w.QueryDNSFail:
            resultTag = QueryIPFailed
            goto EndInfo
        case <-timeoutTimer.C:
            w.Errorf("after %d seconds, requests still not completed, abort", Timeout)
            resultTag = Abort
            goto EndInfo
        }
    }

EndInfo:
    switch resultTag {
    case Completed:
        end := time.Now().UnixNano() / 1e6
        w.Infof("completed %d requests, totally consumed %d ms", RequestCount, end-start)
    case QueryIPFailed:
        w.Errorf("query DNS failed, please check target domain or try again later")
    case Abort:
        w.Errorf("try %d second, the requests still failed to complete, please check target domain or try again later", Timeout)
    }
    return resultTag
}

// GetIPs
// Get IPs of target domain by using golang library net.LookupHost
func (w *Worker) GetIPs(name string) ([]string, error) {
    ips := make([]string, 0)
    var e error
    for i := 0; i < RetryTimes; i++ {
        ips, e = net.LookupHost(name)
        if e == nil && len(ips) > 0 {
            w.Debugf("ips %v", ips)
            break
        }
    }
    w.Debugf("IPs including IPv6: %v", ips)
    if len(ips) == 0 {
        return nil, errors.New("ip not found")
    }
    ips2 := make([]string, 0)
    for _, v := range ips {
        if nil != net.ParseIP(v).To4() {
            ips2 = append(ips2, v)
        }
    }
    //w.Debugf("query domain name:%s, got result: IPs exclude IPv6:%v", name, ips2)
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
                Timeout:   5 * time.Second,
                KeepAlive: 30 * time.Second,
            }).DialContext,
        },
        Timeout: time.Duration(5) * time.Second,
    }
    for {
        select {
        case n := <-receiveCh:
            w.Debugf("IP:%s, received request:%d", ip, n)
            go func(x int) {
                w.Ac.Locker.Lock()
                w.Ac.ActiveConnectionCount[index]++
                w.Debugf("in ip %s goroutine: w.Ac.ActiveConnectionCount[%d] is %d", ip, index, w.Ac.ActiveConnectionCount[index])
                w.Ac.Locker.Unlock()
                req, e := http.NewRequest("Get", fmt.Sprintf("http://%s", ip), nil)
                req.Host = w.Cfg.Name
                if e != nil {
                    w.Errorf("generate http request failed:%d", x)
                }
                retry := 0
                for retry = 0; retry < RetryTimes; retry++ {
                    resp, err := c.Do(req)
                    if err != nil {
                        w.Errorf("%s:get response failed due to %s, retry request:%d", ip, err.Error(), x)
                    } else {
                        if resp.StatusCode >= 500 {
                            w.Errorf("request:%d return status code >500,retry", x)
                            continue
                        }

                        defer resp.Body.Close()
                        finishCh <- x
                        w.Ac.Locker.Lock()
                        w.Ac.ActiveConnectionCount[index]--
                        w.Ac.Locker.Unlock()
                        break
                    }

                }
                if retry >= RetryTimes {
                    w.Debugf("request: %d, after %d retries, still failed, resubmit to global reqchan", n, RetryTimes)
                    requestCh <- x
                }
            }(n)
        }
    }
}

// FlushDNSCache flush DNS Cache to avoid the program use OS dns cache. Ensure the program get a DNS query result from remote
func (w *Worker) FlushDNSCache() {
    command := ""
    switch runtime.GOOS {
    case "windows":
        command = `ipconfig /flushdns`
    case "darwin":
        command = `sudo killall -HUP mDNSResponder`
    case "linux":
        // do nothing since most linux distribution does not have dns cache service pre-installed
    }
    cmd := exec.Command("sh", "-c", command)
    if _, err := cmd.CombinedOutput(); err != nil {
        w.Debugf("execute command:%s failed due to %s", command, err.Error())
    } else {
        //w.Debugf("flush dns stdout/stderr:%s", o)
    }
}
