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
	ID              string `json:"id"`
	Name            string `json:"name"`
	ChannelUsername string `json:"channel_username"`
	ChatID          int64  `json:"chat_id"`
	Tracker         string `json:"tracker"`
	Export          bool   `json:"export"`
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
		return Texporter{}, err
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

func (t Texporter) sendTextToChannel(chatID int64, text string) error {
	msg := tgbotapi.NewMessage(chatID, text)

	_, err := t.TelegramBot.Send(msg)
	if err != nil {
		return err
	}

	return nil
}

func (t Texporter) exportAllTimeEntries() {
	for _, p := range t.ProjectList {
		if !p.Export {
			continue
		}

		// key - user id; value - message to export
		messages := make(map[int64]string)

		timeEntries := t.getListTimeEntries(prevWorkDate(), p.ID)

		for _, te := range timeEntries {
			_, ok := messages[te.User.ID]
			if !ok {
				mess := fmt.Sprintf(
					"%v\n%v\n%v\n%v #%v: %v\n",
					te.SpentOn,
					te.User.Name,
					te.Activity.Name,
					p.Tracker,
					te.Issue.ID,
					te.Comments,
				)

				messages[te.User.ID] = mess
			} else {
				mess := fmt.Sprintf(
					"%v\n%v #%v: %v\n",
					te.Activity.Name,
					p.Tracker,
					te.Issue.ID,
					te.Comments,
				)

				messages[te.User.ID] += mess
			}
		}

		for _, mess := range messages {
			t.sendTextToChannel(p.ChatID, mess)
		}
	}
}

func main() {
	t, err := NewTexporter()
	if err != nil {
		log.Fatal(err)
	}

	t.exportAllTimeEntries()

	// updateConfig := tgbotapi.NewUpdate(0)

	// updateConfig.Timeout = 30

	// updates := t.TelegramBot.GetUpdatesChan(updateConfig)

	// for update := range updates {

	// 	if update.Message == nil {
	// 		continue
	// 	}
	// }
}
