package main

import (
    "flag"
    "fmt"
    worker "github.com/josephzhong333/clienttool/pkg"
    "os"
    "regexp"
    "strings"
)

var (
    name string = ""
    logLevel string = ""
)

func main(){
    flag.StringVar(&name, "name", "", "target domain name")
    flag.StringVar(&logLevel, "log", "info", "log level")
    if !flag.Parsed() {
        flag.Parse()
    }
    if !ValidateDomain(name){
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
        Name: name,
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
--log: Specify the log level. Only there levels supported: info, debug, error. If not specified, use debug level info as default

`)
}

// Validate if the target domain name is legal. If legal, return true; else return false
func ValidateDomain(name string) bool {
    if len(name) == 0 || len(name) > 255 {
        return false
    }
    RegExp := regexp.MustCompile(`^(([a-zA-Z]{1})|([a-zA-Z]{1}[a-zA-Z]{1})|([a-zA-Z]{1}[0-9]{1})|([0-9]{1}[a-zA-Z]{1})|([a-zA-Z0-9][a-zA-Z0-9-_]{1,61}[a-zA-Z0-9]))\.([a-zA-Z]{2,6}|[a-zA-Z0-9-]{2,30}\.[a-zA-Z
 ]{2,3})$`)

    return RegExp.MatchString(name)
}