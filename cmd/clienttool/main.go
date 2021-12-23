package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	valid "github.com/dchest/validator"
	worker "github.com/josephzhong333/clienttool/pkg"
)

var (
	name     string = ""
	logLevel string = ""
)

func main() {
	flag.StringVar(&name, "name", "", "target domain name")
	flag.StringVar(&logLevel, "log", "info", "log level")
	if !flag.Parsed() {
		flag.Parse()
	}
	if !valid.IsValidDomain(name) {
		fmt.Println("Please input correct domain name")
		usage()
		os.Exit(-1)
	}
	ll := strings.ToLower(logLevel)
	if ll != "info" && ll != "debug" && ll != "error" {
		fmt.Println("Please input correct log level")
		usage()
		os.Exit(-1)
	}
	fmt.Printf("domain name:%s, log level:%s\n", name, logLevel)
	w := worker.NewWorker(worker.Config{
		Name:     name,
		LogLevel: logLevel,
	})
	if w != nil {
		w.Run()
	}
}

func usage() {
	fmt.Print(
		`client tool usage:
Example: ./clienttool --name www.ebay.com
Example: ./clienttool --name www.baidu.com --log debug

--name: Specify the name of the target domain name
--log: Specify the log level. Only three levels supported: info, debug, error. If not specified, use log level info as default

`)
}
