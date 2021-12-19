# ClientTool

## To run this tool
### 1, download source code and build
download:
```shell
git clone https://github.com/josephzhong333/clienttool.git
```
build:
```shell
cd clienttool/
go build -o clienttool cmd/clienttool/main.go
```
Above source code can build successfully on Mac and Linux environment, Other platform not tested, not guaranteed
### 2, run clienttool by input command line parameter
The usage of clienttool:
```shell
[root@192-168-4-207 clienttool]# ./clienttool
Please input correct domain name
client tool usage:
Example: ./clienttool --name www.ebay.com
Example: ./clienttool --name www.baidu.com --log debug

--name: Specify the name of the target domain name
--log: Specify the log level. Only three levels supported: info, debug, error. If not specified, use debug level info as default
```
Please note ""--name" option with tartet domain name is mandatory, "--log" is optional.

"--log debug" show verbose log to show the detail processing logic, meanwhile may degrade performance 

“--log info” or no loglevel specified, the program will show concise log


## The design of the program
![](/images/clienttool.png)

As above diagram shows


the process is composed of 6 phases:

1, get IPs of target domain name

2, create corresponding goroutines and channels for each IP. For example, get 3 IPs for one domain name, then create 3 goroutines, and one receive channel for each goroutine 

3, start a load balance goroutine to distribute requests to each IP processing channel. use least active connections algorithm

4, each IP goroutine listen to itself receive channel. start one goroutine for each request. For example, if IP1 goroutine got 20 requests from channel, it will start 20 goroutine to send http request cocurrently.

if timeout or status code >500, the http request goroutine will send the failed request id back to request channel

5, load balance channel distribute the failed request again

6, if all the request completed(completed count>=100), or the timeout time elapsed. the entire process completed

## Load balance algorithm

**Least Active Connections First**

Key advantage:

1, compared to static algorithm like round robin, random, hash, and variants weighted robin, weight random.  static algorithm can not detect the health of backend IP. It need other bypass service to watch the backend IP.

2,Least Active Connections will dynamically feel the load of backend IPs. automatically choose the backend IP which has lower load.   

For example, 3 backend IPs, if one IP always timeout or high latency, marked as IP A.  So IP A will accumulate connection so it's active connections will be high. The LAC algorithm will schedule requests to other backend IPs  

3, The failed request will be resubmit back to the request channel. Load balance algorithm will also choose a workload-low backend to handle the request.  Mostly, the failed request will not distribute the ill backend IP again. It reduces the entire request job latency




