package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

type SlackMessage struct {
	UserID    string    `json:"user_id,omitempty"`
	BotID     string    `json:"bot_id,omitempty"`
	Timestamp time.Time `json:"timestamp,omitempty"`
	Text      string    `json:"text,omitempty"`
}

type SlackIMInfo struct {
	ID     string    `json:"id,omitempty"`
	UserID string    `json:"user_id,omitempty"`
	Latest time.Time `json:"latest,omitempty"`
}

type TsRecord struct {
	Timestamp time.Time `json:"timestamp,omitempty"`
	Text      string    `json:"text,omitempty"`
	SpentHrs  int       `json:"spent_hrs,omitempty"`
}

const ConversationUrlTemplate = "https://%s.slack.com/api/conversations.history?_x_id=noversion-%d.000000&_x_version_ts=noversion&_x_gantry=true"
const ClientBootUrlTemplate = "https://%s.slack.com/api/client.boot?_x_id=noversion-%d.000000&_x_version_ts=noversion&_x_gantry=true"
const GeekbotId = "U2ADJ4J7R"

func main() {
	workspace := flag.String("workspace", "", "slack workspace")
	token := flag.String("token", "xoxc-xxx", "auth token")
	authCookie := flag.String("auth_cookie", "", "auth cookie value (d=<auth_cookie>)")
	from := flag.String("from", time.Now().AddDate(0, -1, 0).Format("2006-01-02"), "from date, example: 2021-01-31")
	flag.Parse()

	fromDate, err := time.Parse("2006-01-02", *from)
	if err != nil {
		log.Fatal(err)
	}

	ims, err := crawlIMList(*token, *authCookie, *workspace)
	if err != nil {
		log.Fatal(err)
	}
	var channel string
	for _, im := range ims {
		if im.UserID == GeekbotId {
			channel = im.ID
		}
	}
	if len(channel) == 0 {
		log.Fatal("Geekbot conversation not found.")
	}

	messages, err := crawlConversationMessages(*token, *authCookie, *workspace, fromDate, channel)
	if err != nil {
		log.Fatal(err)
	}

	ts, err := extractTsRecords(messages)
	if err != nil {
		log.Fatal(err)
	}

	j, err := json.Marshal(ts)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(string(j))
}

func extractTsRecords(messages []*SlackMessage) ([]*TsRecord, error) {
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].Timestamp.Before(messages[j].Timestamp)
	})

	var dateIterator time.Time
	jiraRegex := regexp.MustCompile(`<https://.*?atlassian\.net/browse/[a-zA-Z]+-\d+\|(.+?)>`)
	var tsRecords []*TsRecord
	for _, m := range messages {
		if len(m.BotID) > 0 {
			continue
		}
		//Take only first message of the day
		if m.Timestamp.Year() == dateIterator.Year() && m.Timestamp.YearDay() == dateIterator.YearDay() {
			continue
		}
		dateIterator = m.Timestamp

		tasks := strings.Split(m.Text, "\n")
		cnt := len(tasks)
		for i, task := range tasks {
			var r TsRecord
			task = strings.TrimLeft(task, "• ")
			task = jiraRegex.ReplaceAllString(task, "$1")
			r.Text = task
			r.Timestamp = getPrevWorkingDay(m.Timestamp)
			spent := 8 / cnt
			if i == cnt-1 {
				spent = 8/cnt + 8%cnt
			}
			r.SpentHrs = spent
			tsRecords = append(tsRecords, &r)
		}
	}

	return tsRecords, nil
}

func crawlIMList(token string, authCookie string, workspace string) ([]*SlackIMInfo, error) {
	url := fmt.Sprintf(ClientBootUrlTemplate, workspace, time.Now().Unix())

	payloadParams := map[string]string{
		"build_version_ts":               "1625048244",
		"version_ts":                     "1625048244",
		"flannel_api_ver":                "4",
		"include_min_version_bump_check": "1",
		"only_self_subteams":             "1",
		"token":                          token,
		"_x_reason":                      "deferred-data",
		"_x_sonic":                       "true",
	}

	data, err := postSlackRequest(authCookie, url, &payloadParams)
	if err != nil {
		return nil, err
	}
	//TODO: check for ok=true
	items := gjson.GetBytes(*data, "ims")
	var ims []*SlackIMInfo
	items.ForEach(func(key, value gjson.Result) bool {
		var im SlackIMInfo
		im.ID = value.Get("id").String()
		im.UserID = value.Get("user").String()
		ts, _ := parseSlackTs(value.Get("latest").String())
		im.Latest = *ts
		ims = append(ims, &im)
		return true
	})

	return ims, nil
}

func crawlConversationMessages(token string, authCookie string, workspace string, fromDate time.Time, channelId string) ([]*SlackMessage, error) {
	url := fmt.Sprintf(ConversationUrlTemplate, workspace, time.Now().Unix())
	limit := 1000

	payloadParams := map[string]string{
		"channel":           channelId,
		"limit":             fmt.Sprint(limit),
		"ignore_replies":    "true",
		"include_pin_count": "true",
		"inclusive":         "true",
		"no_user_profile":   "true",
		"token":             token,
		"_x_reason":         "message-pane/requestHistory",
		"_x_mode":           "online",
		"_x_sonic":          "true",
		"oldest":            fmt.Sprintf("%d.000000", fromDate.Unix()),
		//"latest":            "1623919661.000600",
	}

	data, err := postSlackRequest(authCookie, url, &payloadParams)
	if err != nil {
		return nil, err
	}

	//TODO: check for ok = true
	items := gjson.GetBytes(*data, "messages")
	var messages []*SlackMessage
	items.ForEach(func(key, value gjson.Result) bool {
		var m SlackMessage
		m.Text = value.Get("text").String()
		m.UserID = value.Get("user").String()
		m.BotID = value.Get("bot_id").String()
		ts, _ := parseSlackTs(value.Get("ts").String())
		m.Timestamp = *ts
		messages = append(messages, &m)
		return true
	})

	return messages, nil
}

func postSlackRequest(authCookie string, url string, payload *map[string]string) (*[]byte, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	var fw io.Writer
	for key, value := range *payload {
		fw, _ = writer.CreateFormField(key)
		_, _ = io.Copy(fw, strings.NewReader(value))
	}
	writer.Close()
	bytes.NewReader(body.Bytes())
	req, err := http.NewRequest("POST", url, bytes.NewReader(body.Bytes()))
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64; rv:89.0) Gecko/20100101 Firefox/89.0")
	cookie := &http.Cookie{Name: "d", Value: authCookie}
	req.AddCookie(cookie)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return &data, nil
}

func parseSlackTs(ts string) (*time.Time, error) {
	parts := strings.Split(ts, ".")
	if len(parts) < 1 {
		return nil, errors.New("unable to parse timestamp")
	}
	secs, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil, err
	}
	timestamp := time.Unix(secs, 0)
	if len(parts) == 1 {
		return &timestamp, nil
	}
	microsecs, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return nil, err
	}
	timestamp = time.Unix(secs, microsecs*1000)
	return &timestamp, nil
}

func getPrevWorkingDay(date time.Time) time.Time {
	shift := -1
	if date.Weekday() == time.Monday {
		shift = -3
	}
	y, m, d := date.AddDate(0, 0, shift).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
