package worker

import (
    "errors"
    "go.uber.org/zap/zapcore"
    "net"

    "go.uber.org/zap"
)

const (
    retry_times = 3
    timeout = 5
)

type (
	Config struct {
        Name string
        LogLevel string
    }
    Worker struct {
        *zap.SugaredLogger
        IPs map[string]int
        Cfg Config
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
    }
    w := &Worker{
        IPs: make(map[string]int),
        Cfg: config,
    }
    w.SugaredLogger = l.Sugar()
    return w
}


func (w * Worker) Run() {
    ips, e := w.GetIPs(w.Cfg.Name)
    if e != nil {
        w.Error("after retry, still failed to get the IPs for target domain name.Please check the input domain name is correct or try again later.")
    } else {
        for _, v := range ips {
            w.IPs[v] = 0
        }
        w.Infof("found ip addresses:%v", w.IPs)
    }

}


func (w * Worker) GetIPs(name string) ([]string, error) {
    ips := []string{}
    var e error
    for i:=0; i<retry_times; i++ {
        ips, e = net.LookupHost(name)
        if e == nil && len(ips) > 0 {
            break
        }
    }
    w.Debugf("IPs including IPv6", ips)
    if len(ips) == 0 {
        return nil, errors.New("ip not found")
    }
    ips2 := []string{}
    for _, v := range ips {
        if nil != net.ParseIP(v).To4() {
            w.Debugf("ipv6:%s", v)
            ips2 = append(ips2, v)
        }
    }
    w.Debugf("IPs exclude IPv6", ips2)
    if len(ips2) == 0 {
        return nil, errors.New("ip not found")
    } else {
        return ips2, nil
    }
}