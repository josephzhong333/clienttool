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
Above source code can build successfully on Mac(OS X v10.10.4 or later) and Linux environment, Other platform not tested, not guaranteed
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
Please note ""--name" option with target domain name is mandatory, "--log" is optional.

"--log debug" show verbose log to show the detail processing logic, meanwhile may degrade performance 

“--log info” or no loglevel specified, the program will show concise log

If run on Mac, please use "sudo ./clienttool --name xxxx" since the flush DNS cache command in the program require administrator's privilege


## The design of the program
![](/images/clienttool.png)

As above diagram shows

the process is composed of 6 phases:

1, start a dedicated goroutine to continuously query DNS

2, once got a new IP, create a corresponding goroutine and channel for that IP.
Meanwhile, maintain an index of which IP-goroutines will be used according to the latest DNS query result

3, start a load balance goroutine to distribute request to each IP processing channel

4, each goroutine listen to itself channel and send http request, if timeout or status code >500, retry immediately in-place
if retry still failed, send the failed request id back to request channel

5, load balance channel distribute the failed request again

6, if all the request completed(completed count>=100), the entire process completed.

Or the entire process last a Timeout duration, Abort the entire proces


## Load balance algorithm

**Least Active Connections First**

Key advantage:

1, compared to static algorithm like round robin, random, hash, and variants weighted robin, weight random.  static algorithm can not detect the health of backend IP. It need other bypass service to watch the backend IP.

2,Least Active Connections will dynamically feel the load of backend IPs. automatically choose the backend IP which has lower load.   

For example, 3 backend IPs, if one IP always timeout or high latency, marked as IP A.  So IP A will accumulate active count so it's active count will be high. The LAC algorithm will schedule requests to other backend IPs  

3, The failed request will be resubmit back to the request channel. Load balance algorithm will also choose a workload-low backend to handle the request.  Mostly, the failed request will not distribute to the ill backend IP again. It reduces the entire request job latency



