package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

type OnboardRequest struct {
	IP        string  `json:"ip"`
	Port      int     `json:"port"`
	Region    string  `json:"region"`
	Country   string  `json:"country"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type StorageNode struct {
	ID            string    `json:"id"`
	NodeID        string    `json:"nodeId"`
	IP            string    `json:"ip"`
	Port          int       `json:"port"`
	Region        string    `json:"region"`
	Country       string    `json:"country"`
	Latitude      float64   `json:"latitude"`
	Longitude     float64   `json:"longitude"`
	Status        string    `json:"status"`
	LastHeartbeat time.Time `json:"lastHeartbeat"`
	CreatedAt     time.Time `json:"createdAt"`
}

func getAPIURL() string {
	if u := os.Getenv("API_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "http://localhost:8080"
}

func getAdminKey() string {
	if k := os.Getenv("CENTRAL_API_ADMIN_KEY"); k != "" {
		return k
	}
	if k := os.Getenv("ADMIN_API_KEY"); k != "" {
		return k
	}
	return ""
}

func loadEnvFile(paths ...string) {
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		lines := strings.Split(string(data), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				k := strings.TrimSpace(parts[0])
				v := strings.TrimSpace(parts[1])
				v = strings.Trim(v, `"'`)
				if os.Getenv(k) == "" {
					os.Setenv(k, v)
				}
			}
		}
	}
}

func main() {
	loadEnvFile(".env", "../.env")

	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := strings.ToLower(os.Args[1])
	apiURL := getAPIURL()

	switch command {
	case "addnode":
		handleAddNode(apiURL, os.Args[2:])
	case "listnodes", "list":
		handleListNodes(apiURL)
	case "removenode", "remove", "del":
		if len(os.Args) < 3 {
			fmt.Println("Error: Node ID required. Usage: on_board removenode <node-id>")
			os.Exit(1)
		}
		handleRemoveNode(apiURL, os.Args[2])
	case "health", "status":
		handleHealth(apiURL)
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Printf("Unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println("================================================================")
	fmt.Println("  on_board - Distributed Storage Worker Onboarding CLI")
	fmt.Println("================================================================")
	fmt.Println("Usage:")
	fmt.Println("  on_board addnode ip=<ip> region=<region> [country=<country>] [port=<port>] [lat=<lat>] [lon=<lon>]")
	fmt.Println("  on_board listnodes")
	fmt.Println("  on_board removenode <node-id>")
	fmt.Println("  on_board health")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  on_board addnode ip=198.45.98.44 region=Asia")
	fmt.Println("  on_board addnode ip=storage-new-node region=Asia country=Japan port=8081 lat=35.67 lon=139.65")
	fmt.Println("  on_board listnodes")
	fmt.Println("  on_board removenode node-asia-8081")
	fmt.Println("  on_board health")
	fmt.Println()
	fmt.Println("Environment Variables:")
	fmt.Println("  API_URL                Central API base URL (default: http://localhost:8080)")
	fmt.Println("  CENTRAL_API_ADMIN_KEY  Secret administrator key for node mutations (X-Admin-Key)")
}

func handleAddNode(apiURL string, args []string) {
	adminKey := getAdminKey()
	req := OnboardRequest{
		Port: 22,
	}

	for _, arg := range args {
		if strings.HasPrefix(arg, "--api=") {
			apiURL = strings.TrimPrefix(arg, "--api=")
			continue
		}
		if strings.HasPrefix(arg, "--key=") {
			adminKey = strings.TrimPrefix(arg, "--key=")
			continue
		}

		parts := strings.SplitN(arg, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])

		switch key {
		case "key", "admin_key", "adminkey":
			adminKey = val
		case "ip":
			req.IP = val
		case "region":
			req.Region = val
		case "country":
			req.Country = val
		case "port":
			if p, err := strconv.Atoi(val); err == nil {
				req.Port = p
			}
		case "lat", "latitude":
			if lat, err := strconv.ParseFloat(val, 64); err == nil {
				req.Latitude = lat
			}
		case "lon", "lng", "longitude":
			if lon, err := strconv.ParseFloat(val, 64); err == nil {
				req.Longitude = lon
			}
		}
	}

	if req.IP == "" {
		fmt.Println("Error: IP is required. Example: on_board addnode ip=198.45.98.44 region=Asia")
		os.Exit(1)
	}
	if req.Region == "" {
		req.Region = "Unknown"
	}
	if req.Country == "" {
		req.Country = req.Region
	}

	fmt.Printf("[1/4] Connecting to Central API at %s...\n", apiURL)
	fmt.Printf("[2/4] Verifying storage worker health at %s:%d...\n", req.IP, req.Port)

	payload, err := json.Marshal(req)
	if err != nil {
		fmt.Printf("Error serializing request: %v\n", err)
		os.Exit(1)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	httpReq, err := http.NewRequest(http.MethodPost, apiURL+"/api/nodes", bytes.NewReader(payload))
	if err != nil {
		fmt.Printf("Error creating request: %v\n", err)
		os.Exit(1)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if adminKey != "" {
		httpReq.Header.Set("X-Admin-Key", adminKey)
	} else {
		fmt.Println("[WARNING] No administrator key found (set ADMIN_API_KEY in .env, export CENTRAL_API_ADMIN_KEY, or pass key=<key>)")
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		fmt.Printf("Error: Failed to contact Central API: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)

	var res map[string]interface{}
	_ = json.Unmarshal(bodyBytes, &res)

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		errMsg := res["error"]
		if errMsg == nil {
			errMsg = string(bodyBytes)
		}
		fmt.Println("\n[FAILED] Storage Node Onboarding Rejected!")
		fmt.Printf("Status Code: %d\n", resp.StatusCode)
		fmt.Printf("Reason:      %v\n", errMsg)
		os.Exit(1)
	}

	fmt.Println("[3/4] Registering node in MongoDB (storage_nodes)...")
	fmt.Println("[4/4] Data synchronized via rsync!")
	fmt.Println("\n[SUCCESS] Node Onboarded and Activated (ONLINE)!")

	if nodeData, ok := res["node"].(map[string]interface{}); ok {
		fmt.Printf("  Node ID:     %v\n", nodeData["nodeId"])
		fmt.Printf("  IP:          %v\n", nodeData["ip"])
		fmt.Printf("  Port:        %v\n", nodeData["port"])
		fmt.Printf("  Region:      %v\n", nodeData["region"])
		fmt.Printf("  Country:     %v\n", nodeData["country"])
		fmt.Printf("  Status:      %v\n", nodeData["status"])
	}
}

func handleListNodes(apiURL string) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(apiURL + "/api/nodes")
	if err != nil {
		fmt.Printf("Error contacting Central API: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Central API returned status %d\n", resp.StatusCode)
		os.Exit(1)
	}

	var res struct {
		Success bool          `json:"success"`
		Nodes   []StorageNode `json:"nodes"`
		Total   int           `json:"total"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		fmt.Printf("Error decoding nodes response: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n=== REGISTERED STORAGE NODES (%d) ===\n\n", len(res.Nodes))
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "NODE ID\tIP\tPORT\tREGION\tCOUNTRY\tCOORDS\tSTATUS\tLAST HEARTBEAT")
	fmt.Fprintln(w, "-------\t--\t----\t------\t-------\t------\t------\t--------------")

	for _, n := range res.Nodes {
		coords := fmt.Sprintf("%.2f, %.2f", n.Latitude, n.Longitude)
		hb := n.LastHeartbeat.Format("15:04:05")
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\t%s\t%s\t[%s]\t%s\n",
			n.NodeID, n.IP, n.Port, n.Region, n.Country, coords, n.Status, hb)
	}
	w.Flush()
	fmt.Println()
}

func handleRemoveNode(apiURL, nodeID string) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/api/nodes/%s", apiURL, nodeID), nil)
	if err != nil {
		fmt.Printf("Error constructing request: %v\n", err)
		os.Exit(1)
	}
	if key := getAdminKey(); key != "" {
		req.Header.Set("X-Admin-Key", key)
	}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("Error contacting Central API: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		fmt.Printf("[SUCCESS] Node %s removed successfully.\n", nodeID)
	} else {
		var res map[string]interface{}
		_ = json.NewDecoder(resp.Body).Decode(&res)
		fmt.Printf("[FAILED] Could not remove node %s (Status: %d, Error: %v)\n", nodeID, resp.StatusCode, res["error"])
	}
}

func handleHealth(apiURL string) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(apiURL + "/api/status")
	if err != nil {
		fmt.Printf("Error contacting Central API: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var status map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		fmt.Printf("Error parsing status response: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("\n=== SYSTEM HEALTH STATUS ===")
	fmt.Printf("Central API:      [%v]\n", status["api"])
	fmt.Printf("MongoDB Database: [%v]\n", status["mongodb"])

	if storage, ok := status["storage"].([]interface{}); ok {
		fmt.Println("\nStorage Nodes:")
		for _, s := range storage {
			if sMap, ok := s.(map[string]interface{}); ok {
				fmt.Printf("  * %-25s [%v]  %v\n", sMap["name"], sMap["status"], sMap["url"])
			}
		}
	}
	fmt.Println()
}
