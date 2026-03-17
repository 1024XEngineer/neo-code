package config

import (
	"os"
	"strconv"

	"github.com/joho/godotenv"
)

// Config 配置结构体  (我直接照搬了，要改可以删掉也可以留)
type Config struct {
	MySQLRootPassword     string
	MySQLDatabase         string
	RedisPassword         string
	RedisPort             int
	KafkaPort             int
	KafkaJMXPort          int
	KafkaClusterID        string
	KafkaBootstrapServers string
	KafkaZookeeperConnect string
	KafkaTopic            string
	JWTSecret             string
	ModelScopeKey         string
}

// AppConfig 全局配置
var AppConfig *Config

// LoadConfig 加载配置
func LoadConfig() error {
	err := godotenv.Load()
	if err != nil {
	}

	AppConfig = &Config{
		MySQLRootPassword:     os.Getenv("MYSQL_ROOT_PASSWORD"),
		MySQLDatabase:         os.Getenv("MYSQL_DATABASE"),
		RedisPassword:         os.Getenv("REDIS_PASSWORD"),
		KafkaBootstrapServers: os.Getenv("KAFKA_BOOTSTRAP_SERVERS"),
		KafkaZookeeperConnect: os.Getenv("KAFKA_ZOOKEEPER_CONNECT"),
		KafkaClusterID:        os.Getenv("KAFKA_CLUSTER_ID"),
		JWTSecret:             os.Getenv("JWT_SECRET"),
		KafkaTopic:            os.Getenv("KAFKA_TOPIC"),
		ModelScopeKey:         os.Getenv("MODELSCOPE_KEY"),
	}

	// 转换 int 类型
	if portStr := os.Getenv("REDIS_PORT"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			AppConfig.RedisPort = port
		}
	}
	if portStr := os.Getenv("KAFKA_PORT"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			AppConfig.KafkaPort = port
		}
	}
	if portStr := os.Getenv("KAFKA_JMX_PORT"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			AppConfig.KafkaJMXPort = port
		}
	}

	return nil
}
