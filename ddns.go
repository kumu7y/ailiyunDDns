package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/aliyun/alibaba-cloud-sdk-go/services/alidns"
)

// 配置文件结构
type Config struct {
	AccessKey    string `json:"accessKey"`
	AccessSecret string `json:"accessSecret"`
	DomainName   string `json:"domainName"`
	LogPath      string `json:"logPath"`
	APIURL       string `json:"apiURL"`
	RecordType   string `json:"recordType"`
	RR           string `json:"rr"`
	Delay        int    `json:"delay"`
	TimeUnit     string `json:"timeUnit"` // 延迟时间单位
}

// 默认的配置文件内容
var defaultConfig = Config{
	AccessKey:    "your_access_key",
	AccessSecret: "your_access_secret",
	DomainName:   "your_domain_name",
	LogPath:      "DDns.log",
	APIURL:       "https://api.ipify.org/?format=json",
	RecordType:   "A",
	RR:           "*",
	Delay:        1,
	TimeUnit:     "minute",
}

// 自定义的无需更新错误
var ErrNoUpdateNeeded = errors.New("No update needed")

func getPublicIP(apiURL string) (string, error) {
	resp, err := http.Get(apiURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP request failed with status: %s", resp.Status)
	}

	var result map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		return "", err
	}

	ip, ok := result["ip"].(string)
	if !ok {
		return "", errors.New("IP address not found in JSON response")
	}

	return ip, nil
}

func updateDNSRecord(client *alidns.Client, domainName, publicIP, recordType, rr string) error {
	describeRequest := alidns.CreateDescribeDomainRecordsRequest()
	describeRequest.Scheme = "https"
	describeRequest.DomainName = domainName

	// 获取域名的所有解析记录
	records, err := client.DescribeDomainRecords(describeRequest)
	if err != nil {
		return err
	}

	// 遍历解析记录，找到需要更新的记录
	var foundRecord bool
	for _, record := range records.DomainRecords.Record {
		if record.Type == recordType && record.RR == rr {
			// 只有当当前IP和记录IP不一样时才执行更新操作
			if record.Value == publicIP {
				log.Println("Current IP is the same as the record IP. No update needed.")
				return ErrNoUpdateNeeded
			}

			// 找到需要更新的记录，执行更新操作
			updateRequest := alidns.CreateUpdateDomainRecordRequest()
			updateRequest.Scheme = "https"
			updateRequest.RecordId = record.RecordId
			updateRequest.RR = record.RR
			updateRequest.Type = record.Type
			updateRequest.Value = publicIP

			_, err := client.UpdateDomainRecord(updateRequest)
			return err
		}
	}

	// 如果未找到记录，则添加新的 DNS 记录
	if !foundRecord {
		addRequest := alidns.CreateAddDomainRecordRequest()
		addRequest.Scheme = "https"
		addRequest.DomainName = domainName
		addRequest.Type = recordType
		addRequest.RR = rr
		addRequest.Value = publicIP

		_, err := client.AddDomainRecord(addRequest)
		return err
	}

	return fmt.Errorf("DNS record not found")
}

func main() {
	// 通过命令行参数指定配置文件路径，默认为当前目录下的 config.json
	configFilePath := flag.String("config", "config.json", "Path to the configuration file")
	flag.Parse()

	// 检查配置文件是否存在，如果不存在则创建一个默认的配置
	if _, err := os.Stat(*configFilePath); os.IsNotExist(err) {
		saveDefaultConfig(*configFilePath)
		fmt.Printf("Default configuration file '%s' created. Please edit it with your credentials and domain name.\n", *configFilePath)
		os.Exit(0)
	}

	// 从配置文件加载配置
	config, err := loadConfig(*configFilePath)
	if err != nil {
		log.Fatal("Failed to load configuration:", err)
	}

	// 打开日志文件
	logFilePath := config.LogPath
	if logFilePath == "" {
		logFilePath = "DDns.log"
	}

	// 确保日志目录存在
	logDir := filepath.Dir(logFilePath)
	if logDir != "." && logDir != "" {
		err := os.MkdirAll(logDir, 0755)
		if err != nil {
			log.Fatal("Failed to create log directory:", err)
		}
	}

	logFile, err := os.OpenFile(logFilePath, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatal("Failed to open log file:", err)
	}
	defer logFile.Close()

	// 创建一个新的文件Logger
	fileLogger := log.New(logFile, "DDns: ", log.LstdFlags|log.Lmicroseconds)

	client, err := alidns.NewClientWithAccessKey("cn-hangzhou", config.AccessKey, config.AccessSecret)
	if err != nil {
		fileLogger.Fatal("Failed to create Aliyun DNS client:", err)
	}

	// 使用配置中的域名和 API 地址
	domainName := config.DomainName
	apiURL := config.APIURL

	for {
		publicIP, err := getPublicIP(apiURL)

		// 控制台输出
		fmt.Printf("Public IP: %s\n", publicIP)

		if err != nil {
			fileLogger.Println("Failed to get public IP:", err)
		} else {
			err := updateDNSRecord(client, domainName, publicIP, config.RecordType, config.RR)
			if err != nil {
				if err != ErrNoUpdateNeeded {
					fileLogger.Printf("Failed to update DNS record: %v\n", err)
				}
			} else {
				fileLogger.Println("DNS record updated successfully")

				// 控制台输出
				fmt.Println("DNS record updated successfully")
			}
		}

		// 延迟一定时间
		sleepDuration, err := getSleepDuration(config.Delay, config.TimeUnit)
		if err != nil {
			log.Println("Failed to get sleep duration:", err)
			sleepDuration = 1 * time.Minute // 默认延迟1分钟
		}

		time.Sleep(sleepDuration)
	}
}

// 从配置文件加载配置
func loadConfig(filePath string) (Config, error) {
	var config Config
	file, err := os.Open(filePath)
	if err != nil {
		return config, err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	err = decoder.Decode(&config)
	return config, err
}

// 将默认配置保存到文件
func saveDefaultConfig(filePath string) error {
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	err = encoder.Encode(defaultConfig)
	return err
}

// 获取延迟的时间
func getSleepDuration(delay int, timeUnit string) (time.Duration, error) {
	switch timeUnit {
	case "second":
		return time.Duration(delay) * time.Second, nil
	case "minute":
		return time.Duration(delay) * time.Minute, nil
	case "hour":
		return time.Duration(delay) * time.Hour, nil
	default:
		return 0, errors.New("unknown time unit")
	}
}
