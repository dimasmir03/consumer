package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/digilolnet/client3xui"
	"github.com/ilyakaznacheev/cleanenv"
	"github.com/wagslane/go-rabbitmq"
)

type CreateUserTask struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	UUID     string `json:"uuid"`
	Flow     string `json:"flow"`
	Pbk      string `json:"pbk"`
	SID      string `json:"sid"`
	SPX      string `json:"spx"`
}

type Panel struct {
	URL      string `env:"PANEL_URL" default:""`
	Username string `env:"PANEL_USERNAME" default:"admin"`
	Password string `env:"PANEL_PASSWORD" default:"admin"`
}

type Config struct {
	RabbitMQ struct {
		URL          string `env:"RABBITMQ_URL" default:"amqp://guest:guest@localhost:5672/"`
		ExchangeName string `env:"EXCHANGE_NAME" default:"exchange"`
		QueueName    string `env:"QUEUE_NAME" default:"queue"`
	}
	Panel Panel
}

func main() {
	config, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}

	client := client3xui.New(client3xui.Config{
		Url:      config.Panel.URL,
		Username: config.Panel.Username,
		Password: config.Panel.Password,
	})

	conn, err := connectToRabbitMQ(config)
	if err != nil {
		log.Fatalf("error connecting to rabbitmq: %v", err)
	}
	defer conn.Close()

	consumer, err := createConsumer(conn, config)
	if err != nil {
		log.Fatalf("error creating consumer: %v", err)
	}
	defer consumer.Close()

	log.Println("Consumer started")

	err = consumer.Run(handleMessage(client))
	if err != nil {
		log.Fatalf("error running consumer: %v", err)
	}
	log.Println("Consumer stopped")
}

func loadConfig() (Config, error) {
	var config Config
	if err := cleanenv.ReadEnv(&config); err != nil {
		return Config{}, err
	}
	fmt.Println("config loaded successfully")
	fmt.Println("Config: ", config)
	return config, nil
}

func connectToRabbitMQ(config Config) (*rabbitmq.Conn, error) {
	caCert, err := os.ReadFile("/app/cert/ca.crt")
	if err != nil {
		return nil, err
	}

	cert, err := tls.LoadX509KeyPair("/app/cert/client.crt", "/app/cert/client.key")
	if err != nil {
		return nil, err
	}

	rootCAs := x509.NewCertPool()
	rootCAs.AppendCertsFromPEM(caCert)

	tlsConfig := &tls.Config{
		RootCAs:      rootCAs,
		Certificates: []tls.Certificate{cert},
		ServerName:   "rabbitmq", // Optional
	}

	return rabbitmq.NewConn(
		config.RabbitMQ.URL,
		rabbitmq.WithConnectionOptionsLogging,
		rabbitmq.WithConnectionOptionsConfig(rabbitmq.Config{TLSClientConfig: tlsConfig}),
	)
}

func createConsumer(conn *rabbitmq.Conn, config Config) (*rabbitmq.Consumer, error) {
	return rabbitmq.NewConsumer(
		conn,
		config.RabbitMQ.QueueName,
		rabbitmq.WithConsumerOptionsExchangeName(config.RabbitMQ.ExchangeName),
		rabbitmq.WithConsumerOptionsExchangeDeclare,
		rabbitmq.WithConsumerOptionsExchangeKind("fanout"),
		rabbitmq.WithConsumerOptionsBinding(rabbitmq.Binding{
			RoutingKey:     "",
			BindingOptions: rabbitmq.BindingOptions{Declare: true},
		}),
	)
}

func handleMessage(client *client3xui.Client) func(d rabbitmq.Delivery) rabbitmq.Action {
	return func(d rabbitmq.Delivery) rabbitmq.Action {
		var task CreateUserTask
		err := json.Unmarshal(d.Body, &task)
		if err != nil {
			log.Printf("error unmarshalling task: %v, body: %s\n", err, string(d.Body))
			return rabbitmq.NackRequeue
		}
		log.Printf("📥 Получена задача: %+v", task)
		res, err := client.GetInbounds(context.Background())
		if err != nil {
			log.Printf("❌ Ошибка при получении инбаундов: %v", err)
			return rabbitmq.NackDiscard
		}
		if !res.Success {
			log.Printf("❌ Панель вернула ошибку: %s", res.Msg)
			return rabbitmq.NackDiscard
		}
		if len(res.Obj) == 0 {
			log.Printf("❌ Панель вернула пустой результат")
			return rabbitmq.NackDiscard
		}

		if len(res.Obj) > 1 {
			log.Printf("❌ Панель вернула неодну инбаунд")
			return rabbitmq.NackDiscard
		}

		inboundID := res.Obj[0].ID

		users := []client3xui.XrayClient{{
			ID:     task.UUID,
			Email:  task.Username,
			Enable: true,
			TgID:   uint(task.UserID),
			Flow:   task.Flow,
			Pbk:    task.Pbk,
			SID:    task.SID,
			SPX:    task.SPX,
		}}

		res1, err := client.AddClient(context.Background(), uint(inboundID), users)
		if err != nil {
			log.Printf("❌ Ошибка при добавлении клиента %s (%d): %v", task.Username, task.UserID, err)
			return rabbitmq.NackDiscard
		}
		if !res1.Success {
			log.Printf("❌ Панель вернула ошибку для %s (%d): %s", task.Username, task.UserID, res.Msg)
			return rabbitmq.NackDiscard
		}
		log.Printf("✅ Клиент %s успешно добавлен!", task.Username)
		return rabbitmq.Ack
	}
}
