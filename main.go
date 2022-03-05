package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"go.uber.org/zap"
)

type Project struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	ChannelUsername string `json:"channel_username"`
	ChatID          int64  `json:"chat_id"`
	TestChatID      int64  `json:"test_chat_id"`
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
	Logger        *zap.SugaredLogger
}

func NewTexporter() (Texporter, error) {
	t := Texporter{}

	logger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}

	t.Logger = logger.Sugar()

	bytes, err := os.ReadFile("config.json")
	if err != nil {
		return Texporter{}, fmt.Errorf("error during reading 'config.json'\n%v", err)
	}

	config := Config{}
	err = json.Unmarshal(bytes, &config)
	if err != nil {
		return Texporter{}, fmt.Errorf("error during unmarhaling config file\n%v", err)
	}

	t.ProjectList = config.ProjectList

	t.RedmineAPIKey = config.RedmineAPIKey

	t.TelegramBot, err = tgbotapi.NewBotAPI(config.TelegramBotToken)
	if err != nil {
		return Texporter{}, fmt.Errorf("error during Telegram Bot creating\n%v", err)
	}

	t.TelegramBot.Debug = true

	return t, nil
}

func (t Texporter) getListTimeEntries(date string, project string) ([]TimeEntryResponse, error) {
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
		return []TimeEntryResponse{}, fmt.Errorf("error during request creating with url - %v\n%v", url, err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return []TimeEntryResponse{}, fmt.Errorf("error during request doing with request - %v\n%v", req, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return []TimeEntryResponse{}, fmt.Errorf("status code not in 2xx range with request -%v", req)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return []TimeEntryResponse{}, fmt.Errorf("error during read response body\n%v", err)
	}

	teList := TimeEntryListResponse{}

	err = json.Unmarshal(body, &teList)
	if err != nil {
		return []TimeEntryResponse{}, fmt.Errorf("error during unmarshaling body with list time entries response - \n%v", err)
	}

	return teList.TimeEntries, nil
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

func (t Texporter) exportTimeEntries(isTest bool) {
	for _, p := range t.ProjectList {
		if !p.Export {
			continue
		}

		var chatID int64
		if isTest {
			chatID = p.TestChatID
		} else {
			chatID = p.ChatID
		}

		timeEntries, err := t.getListTimeEntries(prevWorkDate(), p.ID)
		if err != nil {
			t.Logger.Errorw("error during get list time entries",
				"Project:", p.Name,
				"Project ID", p.ID,
				"Error text", err,
			)
		}
		// key - user id; value - message text to export
		messages := make(map[int64]string)

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
			err = t.sendTextToChannel(chatID, mess)
			if err != nil {
				t.Logger.Errorw("error during sending message to telegram channel",
					"Project Name", p.Name,
					"Telegram channel ID", chatID,
					"Message text", mess,
					"Error", err,
				)
			} else {
				t.Logger.Infow("success sent message to channel",
					"Project name", p.Name,
					"Telegram channel ID", chatID,
					"Message text", mess,
				)
			}
		}
	}
}

func (t Texporter) botRunAndServe() error {
	updateConfig := tgbotapi.NewUpdate(0)

	updateConfig.Timeout = 30

	updates := t.TelegramBot.GetUpdatesChan(updateConfig)

	for update := range updates {
		if update.Message == nil {
			continue
		}

		chat := update.FromChat()
		switch {
		case chat.IsChannel():
			t.Logger.Info("is channel")
		case chat.IsGroup():
			t.Logger.Info("is group")
		case chat.IsPrivate():
			t.Logger.Info("is private")
		case chat.IsSuperGroup():
			t.Logger.Info("is supergroup")
		}

		user := update.SentFrom()
		t.Logger.Infow("got message",
			"username", user.UserName,
			"user ID", user.ID,
			"is bot", user.IsBot,
		)

		helper := "0. Test\n1. 'Export all'\n2. 'Export BIS'\n3. 'Export EMS'\n4. 'Export SDS'\n"

		msg := tgbotapi.NewMessage(update.Message.Chat.ID, helper)

		switch update.Message.Text {
		case "Test":
			t.Logger.Debug("start test export all entries")
			t.exportTimeEntries(true)
		case "Export all":
			t.Logger.Info("start export all entries")
			// t.exportTimeEntries(false)
		case "Export BIS":
			t.Logger.Info("start export BIS entries only")
		case "Export EMS":
			t.Logger.Info("start export EMS entries only")
		case "Export SDS":
			t.Logger.Info("start export SDS entries only")
		}

		if _, err := t.TelegramBot.Send(msg); err != nil {
			return err
		}
	}

	return nil
}

func main() {
	t, err := NewTexporter()
	if err != nil {
		t.Logger.Fatal(err)
	}
	defer t.Logger.Sync()

	t.Logger.Fatal(t.botRunAndServe())
}
