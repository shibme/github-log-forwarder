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

var ghlf_user_agent = "GHLF"
var data_dir_path = "data"
var cursor_file_path = "data/cursor.txt"
var rate_limit_refresh_time_file_path = "data/rate_limit.txt"
var rate_limit_refresh_time = get_rate_limit_refresh_time()
var github_token = os.Getenv("GHLF_GITHUB_ENTERPRISE_ADMIN_TOKEN")
var github_enterprise = os.Getenv("GHLF_GITHUB_ENTERPRISE_ID")
var log_forward_endpoint_url = os.Getenv("GHLF_LOGGING_ENDPOINT_URL")
var log_forward_endpoint_auth_header = os.Getenv("GHLF_LOGGING_ENDPOINT_AUTH_HEADER")
var processing_interval = os.Getenv("GHLF_PROCESSING_INTERVAL")

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

func check_rate_limit() {
	if time.Now().UnixNano() < rate_limit_refresh_time.UnixNano() {
		log.Println("The ship needs to wait until " + rate_limit_refresh_time.String() + " as it has hit an iceberg...")
		os.Exit(1)
	}
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

	check_rate_limit()

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
	sync_rate_limit(rate_limit, rate_limit_reset_time)
	json.Unmarshal([]byte(resp.Body()), &logs)
	return logs, before, after, rate_limit, rate_limit_reset_time, err
}

func get_github_client() *resty.Client {
	github_api_endpoint := "https://api.github.com"
	if github_token == "" {
		log.Println("Please set GHLF_GITHUB_ENTERPRISE_ADMIN_TOKEN")
		os.Exit(1)
	}
	if github_enterprise == "" {
		log.Println("Please set GHLF_GITHUB_ENTERPRISE_ID")
		os.Exit(1)
	}

	return resty.New().SetHostURL(github_api_endpoint).
		SetAuthToken(github_token).
		SetHeader("User-Agent", ghlf_user_agent).
		SetHeader("Accept", "application/vnd.github.v3+json")
}

func get_log_forward_client() *resty.Client {
	if log_forward_endpoint_url == "" || log_forward_endpoint_auth_header == "" {
		log.Println("Please set GHLF_LOGGING_ENDPOINT_URL and GHLF_LOGGING_ENDPOINT_AUTH_HEADER to forward logs.")
		os.Exit(1)
	}
	return resty.New().SetHostURL(log_forward_endpoint_url).
		SetHeader("User-Agent", ghlf_user_agent).
		SetHeader("Authorization", log_forward_endpoint_auth_header)
}

func postLogs(log_forward_client resty.Client, logs []map[string]interface{}) {
	log_forward_client.R().
		SetBody(logs).
		Post("")
}

func check(e error) {
	if e != nil {
		panic(e)
	}
}

func persist_data_to_file(data_file_path string, content string) {
	if _, err := os.Stat(data_dir_path); os.IsNotExist(err) {
		check(os.Mkdir(data_dir_path, 0777))
	}
	cursor_bytes := []byte(content)
	check(ioutil.WriteFile(data_file_path, cursor_bytes, 0644))
}

func get_data_from_file(data_file_path string) string {
	if _, err := os.Stat(data_file_path); err == nil {
		dat, err := ioutil.ReadFile(data_file_path)
		if err == nil {
			return string(dat)
		}
	}
	return ""
}

func persist_cursor(cursor string) {
	log.Println("Persisting cursor: " + cursor)
	persist_data_to_file(cursor_file_path, cursor)
}

func get_last_cursor() string {
	return get_data_from_file(cursor_file_path)
}

func get_rate_limit_refresh_time() time.Time {
	rate_limit_refresh_time_str := get_data_from_file(rate_limit_refresh_time_file_path)
	if rate_limit_refresh_time_str == "" {
		return time.Unix(0, 0*int64(time.Nanosecond))
	} else {
		refresh_time, err := strconv.ParseInt(rate_limit_refresh_time_str, 10, 64)
		if err == nil {
			return time.Unix(0, refresh_time*int64(time.Nanosecond))
		} else {
			return time.Unix(0, 0*int64(time.Nanosecond))
		}
	}
}

func get_log_time(log map[string]interface{}) time.Time {
	log_time := int64(log["@timestamp"].(float64))
	return time.Unix(0, log_time*int64(time.Millisecond))
}

func sync_rate_limit(rate_limit int, next_limit_refresh_time time.Time) {
	if rate_limit <= 1 {
		rate_limit_refresh_time = next_limit_refresh_time
		persist_data_to_file(rate_limit_refresh_time_file_path, strconv.Itoa(int(rate_limit_refresh_time.UnixNano())))
	}
}

func process_recent_logs(github_client resty.Client, log_forward_client resty.Client) {
	cursor := get_last_cursor()
	if cursor == "" {
		log.Println("No bookmark found locally. Starting fresh...")
		_, _, after, _, _, _ := get_enterprise_logs(github_client, github_enterprise, "", "", "")
		if after != "" {
			persist_cursor(after)
			process_recent_logs(github_client, log_forward_client)
		} else {
			log.Println("The \"after\" cursor was supposed to be available but was not returned")
			os.Exit(1)
		}
	} else {
		logs, _, after, _, _, _ := get_enterprise_logs(github_client, github_enterprise, "asc", "", cursor)
		last_log := logs[len(logs)-1]
		last_log_time := get_log_time(last_log)
		if after == "" {
			log.Println("No new logs to process...")
		} else {
			log.Println("Attempting to process logs after: " + last_log_time.String() + " [Cursor: " + cursor + "]")
		}
		for after != "" {
			persist_cursor(after)
			logs, _, after, _, _, _ = get_enterprise_logs(github_client, github_enterprise, "asc", "", after)
			log.Printf("Pushing logs (%d events): From "+get_log_time(logs[0]).String()+" to "+get_log_time(logs[len(logs)-1]).String()+"\n", len(logs))
			postLogs(log_forward_client, logs)
		}
	}
}

func main() {
	github_client := get_github_client()
	log_forward_client := get_log_forward_client()
	interval, err := strconv.ParseInt(processing_interval, 10, 64)
	if err == nil && interval > 0 {
		for true {
			process_recent_logs(*github_client, *log_forward_client)
			log.Printf("Waiting for %d seconds before next iteration...", interval)
			time.Sleep(time.Duration(interval) * time.Second)
		}
	} else {
		process_recent_logs(*github_client, *log_forward_client)
	}
}
