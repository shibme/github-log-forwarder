package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

var cursor_file_path = "data/cursor.txt"
var cursor_dir_path = "data"
var log_forward_endpoint_url = os.Getenv("GH_LOGS_FORWARD_ENDPOINT_URL")
var log_forward_endpoint_auth_token = os.Getenv("GH_LOGS_FORWARD_ENDPOINT_AUTH_TOKEN")
var github_enterprise = os.Getenv("GH_LOGS_ENTERPRISE_ID")

func extract_url(link string) string {
	split := strings.Split(link, "<")
	if len(split) > 1 && strings.Contains(split[1], "://") {
		return strings.Split(split[1], ">")[0]
	}
	return ""
}

func get_param_from_link(link string, query_key string) string {
	url_str := extract_url(link)
	link_url, _ := url.Parse(url_str)
	queries, _ := url.ParseQuery(link_url.RawQuery)
	return queries[query_key][0]
}

func get_before_after(link_header string) (before string, after string) {
	links := strings.Split(link_header, ",")
	for _, link := range links {
		if strings.Contains(link, "rel=\"prev\"") {
			before = get_param_from_link(link, "before")
		}
		if strings.Contains(link, "rel=\"next\"") {
			after = get_param_from_link(link, "after")
		}
	}
	return before, after
}

func get_enterprise_logs(
	github_client resty.Client,
	enterprise string,
	order string,
	before_cursor string,
	after_cursor string) (
	logs []map[string]interface{},
	before string, after string,
	rate_limit int, rate_limit_reset_time time.Time, err error) {

	resp, err := github_client.R().
		SetQueryParams(map[string]string{
			"per_page": "100",
			"include":  "all",
			"order":    order,
			"before":   before_cursor,
			"after":    after_cursor,
		}).
		Get("/enterprises/" + enterprise + "/audit-log")

	rate_limit, _ = strconv.Atoi(resp.Header().Get("X-RateLimit-Remaining"))
	rate_limit_reset_ts, _ := strconv.ParseInt(resp.Header().Get("X-RateLimit-Reset"), 10, 64)
	rate_limit_reset_time = time.Unix(0, rate_limit_reset_ts*int64(time.Second))
	before, after = get_before_after(resp.Header().Get("Link"))

	json.Unmarshal([]byte(resp.Body()), &logs)
	return logs, before, after, rate_limit, rate_limit_reset_time, err
}

func get_github_client() *resty.Client {
	github_api_endpoint := "https://api.github.com"
	github_token := os.Getenv("GH_LOGS_ENTERPRISE_ADMIN_TOKEN")
	if github_token == "" {
		log.Println("Please set GH_LOGS_ENTERPRISE_ADMIN_TOKEN")
		os.Exit(1)
	}
	if github_enterprise == "" {
		log.Println("Please set GH_LOGS_ENTERPRISE_ID")
		os.Exit(1)
	}

	return resty.New().SetHostURL(github_api_endpoint).
		SetAuthToken(github_token).
		SetHeader("Accept", "application/vnd.github.v3+json")
}

func get_log_forward_client() *resty.Client {
	if log_forward_endpoint_url == "" || log_forward_endpoint_auth_token == "" {
		log.Println("Please set GH_LOGS_FORWARD_ENDPOINT_URL and GH_LOGS_FORWARD_ENDPOINT_AUTH_TOKEN to forward logs. Gracefully exiting!!")
		os.Exit(0)
	}
	return resty.New().SetHostURL(log_forward_endpoint_url).
		SetAuthToken(log_forward_endpoint_auth_token)
}

func postLogs(log_forward_client resty.Client, logs []map[string]interface{}) {
	log_forward_client.R().
		SetQueryParams(map[string]string{
			"per_page": "100",
			"include":  "all",
		}).
		SetBody(logs).
		Post("")
}

func check(e error) {
	if e != nil {
		panic(e)
	}
}

func persist_cursor(cursor string) {
	log.Println("Persisting cursor: " + cursor)
	if _, err := os.Stat(cursor_dir_path); os.IsNotExist(err) {
		check(os.Mkdir(cursor_dir_path, 0777))
	}
	cursor_bytes := []byte(cursor)
	check(ioutil.WriteFile(cursor_file_path, cursor_bytes, 0644))
}

func get_last_cursor() string {
	if _, err := os.Stat(cursor_file_path); err == nil {
		dat, err := ioutil.ReadFile(cursor_file_path)
		if err == nil {
			return string(dat)
		}
	}
	return ""
}

func get_log_time(log map[string]interface{}) time.Time {
	log_time := int64(log["@timestamp"].(float64))
	return time.Unix(0, log_time*int64(time.Millisecond))
}

func process_recent_logs(github_client resty.Client, log_forward_client resty.Client) {
	cursor := get_last_cursor()
	if cursor == "" {
		log.Println("No cursor found. Starting fresh...")
		_, _, after, _, _, _ := get_enterprise_logs(github_client, github_enterprise, "", "", "")
		persist_cursor(after)
		process_recent_logs(github_client, log_forward_client)
	} else {
		logs, _, after, _, _, _ := get_enterprise_logs(github_client, github_enterprise, "asc", "", cursor)
		last_log := logs[len(logs)-1]
		last_log_time := get_log_time(last_log)
		log.Println("Attempting to process logs after: " + last_log_time.String() + " [Cursor: " + cursor + "]")
		for after != "" {
			persist_cursor(after)
			logs, _, after, _, _, _ = get_enterprise_logs(github_client, github_enterprise, "asc", "", after)
			log.Printf("Pushing logs (%d): From "+get_log_time(logs[0]).String()+" to "+get_log_time(logs[len(logs)-1]).String(), len(logs))
			postLogs(log_forward_client, logs)
		}
	}
}

func main() {
	github_client := get_github_client()
	log_forward_client := get_log_forward_client()
	process_recent_logs(*github_client, *log_forward_client)
}
