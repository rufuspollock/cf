package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

const apiBase = "https://api.cloudflare.com/client/v4"

var cachedAPIToken string
var cachedAccountID string

type apiError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type apiResponse struct {
	Success bool            `json:"success"`
	Errors  []apiError      `json:"errors"`
	Result  json.RawMessage `json:"result"`
}

type registrarDomain struct {
	Name      string `json:"name"`
	AutoRenew bool   `json:"auto_renew"`
	Locked    bool   `json:"locked"`
	Privacy   bool   `json:"privacy"`
}

type zone struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

type dnsRecord struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	args := os.Args[1:]
	if len(args) == 0 || isHelp(args[0]) {
		printHelp()
		return nil
	}

	switch args[0] {
	case "wizard":
		return runWizard()
	case "registrar":
		if len(args) > 1 && args[1] == "list" {
			return listRegistrarDomains()
		}
	case "zones":
		if len(args) > 1 {
			switch args[1] {
			case "list":
				return listZones()
			case "add":
				if len(args) < 3 {
					return errors.New("usage: cf zones add <domain>")
				}
				_, err := addZone(args[2])
				return err
			}
		}
	case "dns":
		if len(args) > 1 && args[1] == "add" {
			flags := parseFlags(args[2:])
			zoneName := flags["zone"]
			typeName := strings.ToUpper(flags["type"])
			name := flags["name"]
			content := flags["content"]
			ttl, err := parseIntWithDefault(flags["ttl"], 1)
			if err != nil {
				return fmt.Errorf("invalid --ttl: %w", err)
			}
			proxied := parseBoolWithDefault(flags["proxied"], false)

			if zoneName == "" || typeName == "" || name == "" || content == "" {
				return errors.New("missing required flags for dns add: --zone --type --name --content")
			}

			return addDNSRecord(zoneName, typeName, name, content, ttl, proxied)
		}
	}

	return errors.New("unknown command. run: cf help")
}

func isHelp(v string) bool {
	return v == "help" || v == "--help" || v == "-h"
}

func printHelp() {
	fmt.Println(`cf: Cloudflare domain helper CLI

Usage:
  cf help
  cf wizard
  cf registrar list
  cf zones list
  cf zones add <domain>
  cf dns add --zone <zone-name> --type <A|AAAA|CNAME|TXT|...> --name <record-name> --content <value> [--ttl 1] [--proxied true|false]

Required env vars:
  CF_API_TOKEN or CLOUDFLARE_API_TOKEN
  CF_ACCOUNT_ID or CLOUDFLARE_ACCOUNT_ID
  (or Wrangler login for token fallback)

Examples:
  CF_API_TOKEN=... CF_ACCOUNT_ID=... cf registrar list
  CF_API_TOKEN=... CF_ACCOUNT_ID=... cf wizard
  CF_API_TOKEN=... CF_ACCOUNT_ID=... cf dns add --zone example.com --type A --name @ --content 1.2.3.4 --proxied false`)
}

func requestCF(method, path string, body any) (apiResponse, error) {
	var out apiResponse
	token, err := resolveAPIToken()
	if err != nil {
		return out, err
	}

	fullURL := apiBase + path
	var reqBody io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return out, err
		}
		reqBody = bytes.NewBuffer(payload)
	}

	req, err := http.NewRequest(method, fullURL, reqBody)
	if err != nil {
		return out, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, err
	}

	if resp.StatusCode >= 400 || !out.Success {
		return out, formatAPIErrors(out.Errors, resp.StatusCode)
	}

	return out, nil
}

func resolveAPIToken() (string, error) {
	if cachedAPIToken != "" {
		return cachedAPIToken, nil
	}

	if v := strings.TrimSpace(os.Getenv("CF_API_TOKEN")); v != "" {
		cachedAPIToken = v
		return v, nil
	}

	if v := strings.TrimSpace(os.Getenv("CLOUDFLARE_API_TOKEN")); v != "" {
		cachedAPIToken = v
		return v, nil
	}

	token, err := tokenFromWrangler()
	if err == nil && token != "" {
		cachedAPIToken = token
		return token, nil
	}

	return "", errors.New("missing API token. set CF_API_TOKEN (or CLOUDFLARE_API_TOKEN), or login via Wrangler")
}

func resolveAccountID() (string, error) {
	if cachedAccountID != "" {
		return cachedAccountID, nil
	}

	if v := strings.TrimSpace(os.Getenv("CF_ACCOUNT_ID")); v != "" {
		cachedAccountID = v
		return v, nil
	}

	if v := strings.TrimSpace(os.Getenv("CLOUDFLARE_ACCOUNT_ID")); v != "" {
		cachedAccountID = v
		return v, nil
	}

	token, err := resolveAPIToken()
	if err != nil {
		return "", err
	}

	accountID, err := inferAccountIDFromMemberships(token)
	if err != nil {
		return "", err
	}

	cachedAccountID = accountID
	return accountID, nil
}

func tokenFromWrangler() (string, error) {
	cmd := exec.Command("wrangler", "auth", "token", "--json")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	var parsed struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return "", err
	}
	if strings.TrimSpace(parsed.Token) == "" {
		return "", errors.New("wrangler token output did not include token field")
	}
	return parsed.Token, nil
}

func inferAccountIDFromMemberships(token string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, apiBase+"/memberships", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var payload struct {
		Success bool       `json:"success"`
		Errors  []apiError `json:"errors"`
		Result  []struct {
			Account struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"account"`
		} `json:"result"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 || !payload.Success {
		return "", formatAPIErrors(payload.Errors, resp.StatusCode)
	}

	if len(payload.Result) == 0 {
		return "", errors.New("no Cloudflare account memberships found for token")
	}
	if len(payload.Result) == 1 {
		return payload.Result[0].Account.ID, nil
	}

	choices := make([]string, 0, len(payload.Result))
	for _, item := range payload.Result {
		choices = append(choices, fmt.Sprintf("%s (%s)", item.Account.Name, item.Account.ID))
	}
	return "", fmt.Errorf("multiple accounts found; set CF_ACCOUNT_ID. available: %s", strings.Join(choices, ", "))
}

func formatAPIErrors(errs []apiError, status int) error {
	if len(errs) == 0 {
		return fmt.Errorf("Cloudflare API request failed (HTTP %d)", status)
	}
	parts := make([]string, 0, len(errs))
	for _, e := range errs {
		parts = append(parts, fmt.Sprintf("%d: %s", e.Code, e.Message))
	}
	return errors.New(strings.Join(parts, "; "))
}

func listRegistrarDomains() error {
	accountID, err := resolveAccountID()
	if err != nil {
		return err
	}

	resp, err := requestCF(http.MethodGet, "/accounts/"+accountID+"/registrar/domains", nil)
	if err != nil {
		return err
	}

	var domains []registrarDomain
	if err := json.Unmarshal(resp.Result, &domains); err != nil {
		return err
	}

	if len(domains) == 0 {
		fmt.Println("No registrar domains found in this account.")
		return nil
	}

	for _, d := range domains {
		fmt.Printf("%s  auto_renew=%t  locked=%t  privacy=%t\n", d.Name, d.AutoRenew, d.Locked, d.Privacy)
	}
	return nil
}

func listZones() error {
	accountID, err := resolveAccountID()
	if err != nil {
		return err
	}

	path := "/zones?account.id=" + url.QueryEscape(accountID) + "&per_page=100"
	resp, err := requestCF(http.MethodGet, path, nil)
	if err != nil {
		return err
	}

	var zones []zone
	if err := json.Unmarshal(resp.Result, &zones); err != nil {
		return err
	}

	if len(zones) == 0 {
		fmt.Println("No zones found in this account.")
		return nil
	}

	for _, z := range zones {
		fmt.Printf("%s  status=%s  id=%s\n", z.Name, z.Status, z.ID)
	}

	return nil
}

func getZoneByName(name string) (*zone, error) {
	accountID, err := resolveAccountID()
	if err != nil {
		return nil, err
	}

	path := "/zones?account.id=" + url.QueryEscape(accountID) + "&name=" + url.QueryEscape(name) + "&per_page=1"
	resp, err := requestCF(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var zones []zone
	if err := json.Unmarshal(resp.Result, &zones); err != nil {
		return nil, err
	}

	if len(zones) == 0 {
		return nil, nil
	}

	return &zones[0], nil
}

func addZone(domain string) (*zone, error) {
	accountID, err := resolveAccountID()
	if err != nil {
		return nil, err
	}

	resp, err := requestCF(http.MethodPost, "/zones", map[string]any{
		"account":    map[string]string{"id": accountID},
		"jump_start": true,
		"name":       domain,
		"type":       "full",
	})
	if err == nil {
		var z zone
		if unmarshalErr := json.Unmarshal(resp.Result, &z); unmarshalErr != nil {
			return nil, unmarshalErr
		}
		fmt.Printf("Zone created: %s (id=%s, status=%s)\n", z.Name, z.ID, z.Status)
		return &z, nil
	}

	if strings.Contains(err.Error(), "1061") {
		existing, existingErr := getZoneByName(domain)
		if existingErr != nil {
			return nil, existingErr
		}
		if existing != nil {
			fmt.Printf("Zone already exists: %s (id=%s, status=%s)\n", existing.Name, existing.ID, existing.Status)
			return existing, nil
		}
	}

	return nil, err
}

func addDNSRecord(zoneName, typeName, name, content string, ttl int, proxied bool) error {
	z, err := getZoneByName(zoneName)
	if err != nil {
		return err
	}
	if z == nil {
		return fmt.Errorf("zone not found for %s. run: cf zones add %s", zoneName, zoneName)
	}

	resp, err := requestCF(http.MethodPost, "/zones/"+z.ID+"/dns_records", map[string]any{
		"type":    typeName,
		"name":    name,
		"content": content,
		"ttl":     ttl,
		"proxied": proxied,
	})
	if err != nil {
		return err
	}

	var r dnsRecord
	if err := json.Unmarshal(resp.Result, &r); err != nil {
		return err
	}

	fmt.Printf("DNS record created: %s %s -> %s (id=%s)\n", r.Type, r.Name, r.Content, r.ID)
	return nil
}

func parseFlags(args []string) map[string]string {
	out := map[string]string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			continue
		}

		if idx := strings.Index(arg, "="); idx != -1 {
			out[strings.TrimPrefix(arg[:idx], "--")] = arg[idx+1:]
			continue
		}

		key := strings.TrimPrefix(arg, "--")
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			out[key] = args[i+1]
			i++
		} else {
			out[key] = "true"
		}
	}
	return out
}

func parseBoolWithDefault(v string, fallback bool) bool {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return strings.EqualFold(v, "true") || strings.EqualFold(v, "yes") || v == "1"
}

func parseIntWithDefault(v string, fallback int) (int, error) {
	if strings.TrimSpace(v) == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, err
	}
	return n, nil
}

func runWizard() error {
	reader := bufio.NewReader(os.Stdin)
	domain, err := prompt(reader, "Domain you want to onboard (example.com)", "")
	if err != nil {
		return err
	}
	if domain == "" {
		return errors.New("domain is required")
	}

	alreadyRegistered, err := promptYesNo(reader, "Is this domain already registered somewhere?", true)
	if err != nil {
		return err
	}

	if !alreadyRegistered {
		dashboardURL := "https://dash.cloudflare.com/?to=/:account/domains"
		fmt.Println("\nManual step required: register domain in Cloudflare Dashboard:")
		fmt.Println(dashboardURL)

		openNow, err := promptYesNo(reader, "Open the dashboard URL in your browser now?", true)
		if err != nil {
			return err
		}
		if openNow {
			if err := openURL(dashboardURL); err != nil {
				fmt.Printf("Could not open browser automatically: %v\n", err)
			} else {
				fmt.Println("Opened browser tab.")
			}
		}

		if _, err := prompt(reader, "Press Enter when registration is complete and you want to continue", ""); err != nil {
			return err
		}
	}

	addZoneNow, err := promptYesNo(reader, fmt.Sprintf("Add %s as a zone in Cloudflare now?", domain), true)
	if err != nil {
		return err
	}
	if addZoneNow {
		z, err := addZone(domain)
		if err != nil {
			return err
		}
		if z != nil && z.Status != "active" {
			fmt.Printf("Zone status is '%s'. You may still need to update nameservers at your current registrar.\n", z.Status)
		}
	}

	for {
		addRecord, err := promptYesNo(reader, "Add a DNS record now?", true)
		if err != nil {
			return err
		}
		if !addRecord {
			break
		}

		zoneName, err := prompt(reader, "Zone name", domain)
		if err != nil {
			return err
		}
		typeName, err := prompt(reader, "Record type", "A")
		if err != nil {
			return err
		}
		name, err := prompt(reader, "Record name", "@")
		if err != nil {
			return err
		}
		content, err := prompt(reader, "Record content (IP or hostname)", "")
		if err != nil {
			return err
		}
		ttlRaw, err := prompt(reader, "TTL (1 means auto)", "1")
		if err != nil {
			return err
		}
		ttl, err := strconv.Atoi(ttlRaw)
		if err != nil {
			return fmt.Errorf("invalid TTL: %w", err)
		}
		proxied, err := promptYesNo(reader, "Proxied through Cloudflare (orange cloud)?", false)
		if err != nil {
			return err
		}

		if err := addDNSRecord(zoneName, strings.ToUpper(typeName), name, content, ttl, proxied); err != nil {
			return err
		}
	}

	fmt.Println("\nWizard complete.")
	return nil
}

func prompt(reader *bufio.Reader, question, fallback string) (string, error) {
	suffix := ""
	if fallback != "" {
		suffix = " [" + fallback + "]"
	}
	fmt.Printf("%s%s: ", question, suffix)
	text, err := reader.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return fallback, nil
	}
	return text, nil
}

func promptYesNo(reader *bufio.Reader, question string, fallback bool) (bool, error) {
	defaultLabel := "y/N"
	if fallback {
		defaultLabel = "Y/n"
	}
	v, err := prompt(reader, fmt.Sprintf("%s (%s)", question, defaultLabel), "")
	if err != nil {
		return false, err
	}
	if v == "" {
		return fallback, nil
	}
	if strings.EqualFold(v, "y") || strings.EqualFold(v, "yes") {
		return true, nil
	}
	if strings.EqualFold(v, "n") || strings.EqualFold(v, "no") {
		return false, nil
	}
	return fallback, nil
}

func openURL(rawURL string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", rawURL)
	default:
		cmd = exec.Command("xdg-open", rawURL)
	}
	return cmd.Start()
}
