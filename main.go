package main

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/websocket"
)

var (
	token string
	addr  string

	sc        = bufio.NewScanner(os.Stdin)
	uriFormat = "wss://%s/api/v1/namespaces/%s/pods/%s/exec?command=/bin/sh&stdin=true&stderr=true&stdout=true&tty=true"
)

func init() {
	// 下記コマンドでトークンを取得：
	// SECRET_NAME=$(kubectl get serviceaccount default -o jsonpath='{.secrets[0].name}')
	// TOKEN=$(kubectl get secret $SECRET_NAME -o jsonpath='{.data.token}' | base64 --decode)
	// echo $TOKEN
	if token = os.Getenv("K8S_TOKEN"); token == "" {
		fmt.Println("error: K8S_TOKEN value is empty.")
		os.Exit(0)
	}

	// 下記コマンドでk8sアドレスを取得：
	// kubectl config view --minify -o jsonpath='{.clusters[0].cluster.server}'
	if addr = os.Getenv("K8S_ADDR"); addr == "" {
		fmt.Println("error: K8S_ADDR value is empty.")
		os.Exit(0)
	}
}

type k8sSock struct {
	token string

	addr string
	uri  string
}

func newClient() k8sSock {
	return k8sSock{
		token: token,
		addr:  addr,
	}
}

func (k8s *k8sSock) URI(namespace, pod string) {
	k8s.uri = fmt.Sprintf(uriFormat, k8s.addr, namespace, pod)
}

func (k8s *k8sSock) Start() error {
	requestHeader := newRequestHeader(k8s.token)
	dialer := websocket.DefaultDialer
	dialer.TLSClientConfig = &tls.Config{
		InsecureSkipVerify: true,
	}

	// Connect to target kubernetes
	conn, _, err := dialer.Dial(k8s.uri, requestHeader)
	if err != nil {
		return err
	}
	defer conn.Close()

	// error channel
	errOutChan := make(chan error, 1)
	errInChan := make(chan error, 1)

	// 手元のコンソールからの入力待ち
	go outputListener(conn, errOutChan)
	// k8sコンテナからの出力待ち
	go inputListener(conn, errInChan)

	var message string
	select {
	case err = <-errOutChan:
		message = "error: %v\n"
	case err = <-errInChan:
		message = "error: %v\n"
	}
	if e, ok := err.(*websocket.CloseError); !ok || e.Code == websocket.CloseAbnormalClosure {
		log.Printf(message, err)
	}

	return nil
}

func newRequestHeader(token string) http.Header {
	requestHeader := http.Header{}
	requestHeader.Add("Authorization", "Bearer "+token)

	return requestHeader
}

func outputListener(conn *websocket.Conn, errChan chan error) {
	for {
		cmd := func() []byte {
			sc.Scan()
			cmd := append([]byte{0}, sc.Bytes()...)
			return append(cmd, byte(13))
		}()
		if err := conn.WriteMessage(websocket.BinaryMessage, cmd); err != nil {
			errChan <- err
		}
	}
}

func inputListener(conn *websocket.Conn, errChan chan error) {
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			conn.WriteMessage(websocket.CloseMessage, []byte(err.Error()))
			errChan <- err
			break
		}

		if len(msg) > 1 {
			fmt.Printf(fmt.Sprintf("%s", msg[1:len(msg)]))
		}
	}
}

func main() {
	readLine := func() string {
		sc.Scan()
		return sc.Text()
	}

	fmt.Print("Kubernetes namespace: ")
	namespace := readLine()

	fmt.Print("Kubernetes pod name: ")
	podname := readLine()

	sock := newClient()
	sock.URI(namespace, podname)

	if err := sock.Start(); err != nil {
		log.Fatal(err)
	}
}
