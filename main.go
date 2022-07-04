package main

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"time"

	"golang.org/x/time/rate"
)

// QuestionResponse contains the response struct for the search API
type QuestionResponse struct {
	Items          []*Items `json:"items"`
	HasMore        bool     `json:"has_more"`
	QuotaMax       int      `json:"quota_max"`
	QuotaRemaining int      `json:"quota_remaining"`
}

type Items struct {
	IsAnswered   bool   `json:"is_answered,omitempty"`
	ViewCount    int    `json:"view_count"`
	CreationDate int    `json:"creation_date"`
	QuestionId   int    `json:"question_id"`
	Link         string `json:"link"`
}

// RLHTTPClient Rate Limited HTTP Client.
type RLHTTPClient struct {
	client      *http.Client
	Ratelimiter *rate.Limiter
}

func main() {
	b, err := GetTop5Questions("git", "go")
	if err != nil {
		panic(err)
	}

	fmt.Println(string(b))
}

// Do dispatches the HTTP request to the network.
func (c *RLHTTPClient) Do(
	top5 *[]*Items,
	url string,
	params map[string]string,
	page int,
) error {
	ctx := context.Background()

	if err := c.Ratelimiter.Wait(ctx); err != nil { // This is a blocking call. Honors the rate limit
		return err
	}

	req, err := request(url, params, page)
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	reader, err := gzip.NewReader(resp.Body)
	if err != nil {
		return err
	}

	defer reader.Close()

	qr := &QuestionResponse{}
	if err := json.NewDecoder(reader).Decode(qr); err != nil {
		return err
	}

	items := make([]*Items, 0)

	for _, item := range qr.Items {
		if !item.IsAnswered {
			items = append(items, item)
		}
	}

	items = append(items, *top5...)

	sort.Slice(items, func(i, j int) bool {
		return items[i].ViewCount > items[j].ViewCount
	})

	if len(items) > 5 {
		*top5 = items[:5]
	}

	if qr.HasMore {
		return c.Do(top5, url, params, page+1)
	}

	return nil
}

// NewClient return http client with a ratelimiter
func NewClient(rl *rate.Limiter) *RLHTTPClient {
	c := &RLHTTPClient{
		client:      http.DefaultClient,
		Ratelimiter: rl,
	}

	return c
}

// GetTop5Questions returns json array with the top 5 (view_count) questions
func GetTop5Questions(inTitle, tagged string) ([]byte, error) {
	rl := rate.NewLimiter(rate.Every(1*time.Second), 30) // 30 request every 1 seconds
	c := NewClient(rl)

	url := "https://api.stackexchange.com/2.3/search?order=desc&sort=activity&site=stackoverflow"
	page := 1

	params := make(map[string]string)
	params["intitle"] = inTitle

	if tagged != "" {
		params["tagged"] = tagged
	}

	params["pagesize"] = strconv.Itoa(100)
	params["fromdate"] = strconv.FormatInt(time.Now().Add(-24*time.Hour*365).Unix(), 10)
	params["todate"] = strconv.FormatInt(time.Now().Unix(), 10)

	top5 := make([]*Items, 0)

	if err := c.Do(&top5, url, params, page); err != nil {
		return nil, err
	}

	return json.Marshal(top5)
}

// request composes the http.Request used by the client
func request(url string, params map[string]string, page int) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	params["page"] = strconv.Itoa(page)

	if params != nil {
		q := req.URL.Query()
		for k, v := range params {
			q.Add(k, v)
		}

		req.URL.RawQuery = q.Encode()
	}

	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Content-Type", "application/json")

	return req, nil
}
