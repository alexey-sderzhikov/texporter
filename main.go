package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Project struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Channel string `json:"channel"`
}

type Config struct {
	RedmineAPIKey    string    `json:"redmine_api_key"`
	TelegramBotToken string    `json:"telegram_bot_token"`
	ProjectList      []Project `json:"projects"`
}

type NameAndID struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type ID struct {
	ID int64 `json:"id"`
}

type TimeEntryResponse struct {
	ID        int64     `json:"id"`
	Project   NameAndID `json:"project"`
	Issue     ID        `json:"issue"`
	User      NameAndID `json:"user"`
	Activity  NameAndID `json:"activity"`
	Hours     float32   `json:"hours"`
	Comments  string    `json:"comments"`
	SpentOn   string    `json:"spent_on"`
	CreatedOn string    `json:"created_on"`
	UpdatedOn string    `json:"updated_on"`
}

type TimeEntryListResponse struct {
	TimeEntries []TimeEntryResponse `json:"time_entries"`
	TotalCount  int                 `json:"total_count"`
	Offset      int                 `json:"offset"`
	Limit       int                 `json:"limit"`
}

type Texporter struct {
	RedmineAPIKey string
	TelegramBot   *tgbotapi.BotAPI
	ProjectList   []Project
}

func NewTexporter() (Texporter, error) {
	t := Texporter{}

	bytes, err := os.ReadFile("config.json")
	if err != nil {
		return Texporter{}, err
	}

	config := Config{}
	err = json.Unmarshal(bytes, &config)
	if err != nil {
		return Texporter{}, nil
	}

	t.ProjectList = config.ProjectList

	t.RedmineAPIKey = config.RedmineAPIKey

	t.TelegramBot, err = tgbotapi.NewBotAPI(config.TelegramBotToken)
	if err != nil {
		return Texporter{}, err
	}

	t.TelegramBot.Debug = true

	return t, nil
}

func (t Texporter) getListTimeEntries(date string, project string) []TimeEntryResponse {
	client := &http.Client{}

	url := "https://support.bergen.tech/" + "time_entries.json?key=" + t.RedmineAPIKey

	params := make([]string, 0)
	params = append(params,
		"project_id="+project,
		"spent_on="+date,
	)

	for _, p := range params {
		url += "&" + p
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		log.Fatal(err)
	}

	resp, err := client.Do(req)
	if err != nil {
		log.Fatal(err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		log.Fatal("status code not in 2xx range")
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}

	teList := TimeEntryListResponse{}

	err = json.Unmarshal(body, &teList)
	if err != nil {
		log.Fatal(err)
	}

	return teList.TimeEntries
}

// detect last work date before today
func prevWorkDate() string {
	today := time.Now()
	if today.Weekday() == time.Monday {
		return today.AddDate(0, 0, -3).Format("2006-01-02")
	}

	return today.AddDate(0, 0, -1).Format("2006-01-02")
}

func (t Texporter) sendTextToChannel(channel string, text string) error {
	msg := tgbotapi.NewMessageToChannel(channel, text)

	_, err := t.TelegramBot.Send(msg)
	if err != nil {
		return err
	}

	return nil
}

func main() {
	t, err := NewTexporter()
	if err != nil {
		log.Fatal(err)
	}

	for _, p := range t.ProjectList {
		timeEntries := t.getListTimeEntries(prevWorkDate(), p.ID)

		for _, te := range timeEntries {
			mess := fmt.Sprintf(
				"%v\n%v\n%v\nInternal #%v: %v",
				te.SpentOn,
				te.User.Name,
				te.Activity.Name,
				te.Issue.ID,
				te.Comments,
			)

			t.sendTextToChannel(p.Channel, mess)
		}
	}

}
