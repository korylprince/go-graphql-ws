package graphql_test

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/korylprince/go-graphql-ws"
)

func ExampleConn_Execute() {
	var query = &graphql.MessagePayloadStart{
		Query: `
		query get_users {
		  user {
			id
			name
		  }
		}
`,
	}

	headers := make(http.Header)
	headers.Add("X-Hasura-Admin-Secret", "test")

	conn, _, err := graphql.DefaultDialer.Dial("ws://localhost:8080/v1/graphql", headers, nil)
	if err != nil {
		log.Fatalln("Unable to connect:", err)
	}

	payload, err := conn.Execute(context.Background(), query)
	if err != nil {
		log.Fatalln("Unable to Execute:", err)
	}
	if len(payload.Errors) > 0 {
		log.Fatalln("Unable to Execute:", payload.Errors)
	}
	log.Println("Payload Received:", string(payload.Data))
}

func ExampleConn_Execute_cancel() {
	var query = &graphql.MessagePayloadStart{
		Query: `
		query get_users {
		  user {
			id
			name
		  }
		}
`,
	}

	headers := make(http.Header)
	headers.Add("X-Hasura-Admin-Secret", "test")

	conn, _, err := graphql.DefaultDialer.Dial("ws://localhost:8080/v1/graphql", headers, nil)
	if err != nil {
		log.Fatalln("Unable to connect:", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	//cancel long running query
	go func() {
		cancel()
	}()

	_, err = conn.Execute(ctx, query)
	log.Println("Canceled:", ctx.Err() == err)
}

func ExampleConn_Subscribe() {
	var subscription = &graphql.MessagePayloadStart{
		Query: `
		subscription get_users {
		  user {
			id
			name
		  }
		}
`,
	}

	headers := make(http.Header)
	headers.Add("X-Hasura-Admin-Secret", "test")

	conn, _, err := graphql.DefaultDialer.Dial("ws://localhost:8080/v1/graphql", headers, nil)
	if err != nil {
		log.Fatalln("Unable to connect:", err)
	}

	id, err := conn.Subscribe(subscription, func(m *graphql.Message) {
		if m.Type == graphql.MessageTypeError {
			err := graphql.ParseError(m.Payload)
			//handle err
			_ = err
			return
		} else if m.Type == graphql.MessageTypeComplete {
			//clean up
			return
		}
		payload := new(graphql.MessagePayloadData)
		if err := json.Unmarshal(m.Payload, payload); err != nil {
			//handle error
			return
		}
		if len(payload.Errors) > 0 {
			//handle error
			return
		}
		log.Println("Payload Received:", string(payload.Data))
	})
	if err != nil {
		log.Fatalln("Unable to Subscribe:", err)
	}

	//do other stuff
	time.Sleep(time.Second * 5)

	if err = conn.Unsubscribe(id); err != nil {
		log.Fatalln("Unable to Unsubscribe:", err)
	}
}
