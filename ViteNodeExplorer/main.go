package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/vitelabs/go-vite/v2/crypto/ed25519"
	"github.com/vitelabs/go-vite/v2/net/discovery"
	"github.com/vitelabs/go-vite/v2/net/vnode"
	"github.com/vitelabs/go-vite/v2/rpc"
)

const (
	rpcPort          = 48132
	discoveryTimeout = 2 * time.Minute
	listenPort       = 8485 // An unused port for our explorer client
)

type BootnodesResponse struct {
	Data []string `json:"data"`
}

func main() {
	fmt.Println("Vite Node Explorer")

	// 1. Fetch bootnodes from the URL
	bootnodes, err := fetchBootnodes("https://bootnodes.vite.net/bootmainnet.json")
	if err != nil {
		log.Fatalf("Failed to fetch bootnodes: %v", err)
	}
	fmt.Printf("Fetched %d bootnodes\n", len(bootnodes))
	for _, bootnode := range bootnodes {
		fmt.Printf("- %s\n", bootnode)
	}

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

	fmt.Printf("Discovery started. Searching for RPC nodes for %v...\n", discoveryTimeout)

	// 4. Periodically check for nodes and collect RPC-enabled ones
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	timeout := time.After(discoveryTimeout)

	rpcNodes := make(map[string]struct{})

	fmt.Println("Searching for nodes...")

	for {
		select {
		case <-ticker.C:
			nodes := d.Nodes()
			fmt.Printf("Discovered %d nodes so far. Checking for RPC...\n", len(nodes))
			for _, n := range nodes {
				host := n.EndPoint.Hostname()
				port := n.EndPoint.Port
				fmt.Printf("Found Node: %s:%d\n", host, port)
				rpcUrl := fmt.Sprintf("http://%s:%d", host, rpcPort)
				if _, exists := rpcNodes[rpcUrl]; !exists {
					if isRpcEnabled(n) {
						fmt.Printf("Found RPC Node: %s\n", rpcUrl)
						rpcNodes[rpcUrl] = struct{}{}
					}
				}
			}
		case <-timeout:
			fmt.Println("\nDiscovery finished.")
			if len(rpcNodes) == 0 {
				fmt.Println("No RPC nodes found.")
			} else {
				fmt.Println("Found the following RPC nodes:")
				for nodeUrl := range rpcNodes {
					fmt.Println("- ", nodeUrl)
				}
			}
			return
		}
	}
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

func isRpcEnabled(node *vnode.Node) bool {
	host := string(node.EndPoint.Host)
	// Skip invalid or local IPs
	if host == "" || host == "127.0.0.1" || host == "0.0.0.0" {
		return false
	}
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
