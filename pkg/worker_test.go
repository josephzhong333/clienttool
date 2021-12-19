package worker

import (
	"fmt"
	"testing"

	valid "github.com/dchest/validator"
)

var (
	siteArray = []string{"www.", "www.ebay.com", "www.sina.com.cn", "11", "--", "+x.", "\\", "3@5.cn"}
	loglevel  = []string{"debug", "info", "error", ""}
)

func TestGetIPs(t *testing.T) {
	for _, ll := range loglevel {
		for _, v := range siteArray {
			ips, e := NewWorker(Config{
				Name:     v,
				LogLevel: ll,
			}).GetIPs(v)
			if e != nil {
				fmt.Printf("query domain:%v failed due to %s\n", v, e.Error())
			} else {
				fmt.Printf("query domain:%v, got ips:%v\n", v, ips)
			}
		}
	}
}

func TestValidateDomain(t *testing.T) {
	for _, v := range siteArray {
		if valid.IsValidDomain(v) {
			fmt.Printf("domain:%v is valid\n", v)
		} else {
			fmt.Printf("domain:%v is NOT valid\n", v)
		}
	}
}

func TestRun(t *testing.T) {
	for _, ll := range loglevel {
		for _, v := range siteArray {
			if valid.IsValidDomain(v) {
				ret := NewWorker(Config{
					Name:     v,
					LogLevel: ll,
				}).Run()
				fmt.Printf("run domain:%v, result: %d\n", v, ret)
			}
		}
	}
}
