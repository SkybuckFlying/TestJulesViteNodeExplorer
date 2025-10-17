package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/vitelabs/go-vite/v2/crypto/ed25519"
	"github.com/vitelabs/go-vite/v2/net/discovery"
	"github.com/vitelabs/go-vite/v2/net/vnode"
	"github.com/vitelabs/go-vite/v2/rpc"
)

const (
	discoveryTimeout = 2 * time.Minute
	listenPort       = 8485 // An unused port for our explorer client
	rpcPort          = 48132
)

type BootnodesResponse struct {
	Data []string `json:"data"`
}

func main() {
	fmt.Println("Vite Node Explorer")

	// 0. Get our own public IP
	publicIP, err := getPublicIP()
	if err != nil {
		log.Printf("Could not determine public IP: %v. (self) tag will not be available.", err)
	} else {
		fmt.Printf("My public IP is: %s\n", publicIP)
	}

	// 1. Fetch bootnodes from the URL
	bootnodes, err := fetchBootnodes("https://bootnodes.vite.net/bootmainnet.json")
	if err != nil {
		log.Fatalf("Failed to fetch bootnodes: %v", err)
	}
	fmt.Printf("Fetched %d bootnodes\n", len(bootnodes))

	// 2. Create a local node with a complete endpoint
	listenAddr := fmt.Sprintf("0.0.0.0:%d", listenPort)
	endpoint, err := vnode.ParseEndPoint(listenAddr)
	if err != nil {
		log.Fatalf("Failed to parse endpoint: %v", err)
	}

	pub, key, err := ed25519.GenerateKey(nil)
	if err != nil {
		log.Fatalf("Failed to generate key: %v", err)
	}
	node := &vnode.Node{
		ID:       vnode.NodeID(pub),
		EndPoint: endpoint,
		Net:      1,
	}

	// 3. Setup and start discovery
	d := discovery.New(key, node, bootnodes, nil, listenAddr, nil)
	err = d.Start()
	if err != nil {
		log.Fatalf("Failed to start discovery: %v", err)
	}
	defer d.Stop()

	fmt.Printf("Discovery started. Searching for nodes for %v...\n", discoveryTimeout)

	// 4. Periodically check for nodes and report their status
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	timeout := time.After(discoveryTimeout)

	processedNodes := make(map[string]bool)

	for {
		select {
		case <-ticker.C:
			nodes := d.Nodes()
			fmt.Printf("\n> Discovered %d nodes. Checking status...\n", len(nodes))
			for _, n := range nodes {
				host := n.EndPoint.Hostname()
				// Skip invalid or local IPs
				if host == "" || host == "127.0.0.1" || host == "0.0.0.0" {
					continue
				}
				addr := fmt.Sprintf("%s:%d", host, n.EndPoint.Port)

				if _, processed := processedNodes[addr]; processed {
					continue
				}

				tag := ""
				if host == publicIP {
					tag = "(self)"
				}

				status := "Offline"
				rpcStatus := "[RPC Disabled]"
				if isNodeOnline(n) {
					status = "Online"
					if isRpcEnabled(n) {
						rpcStatus = "[RPC Enabled]"
					}
				}

				fmt.Printf("- Node: %s %s [%s] %s\n", addr, tag, status, rpcStatus)
				processedNodes[addr] = true
			}
		case <-timeout:
			fmt.Println("\nDiscovery finished.")
			return
		}
	}
}

func getPublicIP() (string, error) {
	resp, err := http.Get("https://api.ipify.org")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	ip, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(ip), nil
}

func fetchBootnodes(url string) ([]string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var bootnodesResponse BootnodesResponse
	err = json.Unmarshal(body, &bootnodesResponse)
	if err != nil {
		return nil, err
	}

	return bootnodesResponse.Data, nil
}

func isNodeOnline(node *vnode.Node) bool {
	address := fmt.Sprintf("%s:%d", node.EndPoint.Hostname(), node.EndPoint.Port)
	conn, err := net.DialTimeout("tcp", address, 2*time.Second)
	if err != nil {
		return false
	}
	defer conn.Close()
	return true
}

func isRpcEnabled(node *vnode.Node) bool {
	host := string(node.EndPoint.Host)
	rpcUrl := fmt.Sprintf("http://%s:%d", host, rpcPort)

	// Use a timeout for the RPC dial
	client, err := rpc.DialHTTPWithClient(rpcUrl, &http.Client{Timeout: 2 * time.Second})
	if err != nil {
		return false
	}
	defer client.Close()

	// A simple check to see if the connection is alive
	var result string
	err = client.Call(&result, "net_version")
	return err == nil
}