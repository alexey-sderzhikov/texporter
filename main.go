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
	Model         model
}

type model struct {
	state  string
	isTest bool
	date   string
}

var typesKeyboard = tgbotapi.NewInlineKeyboardMarkup(
	tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("выгружаем списания", "export"),
	),
	tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("напоминаем списаться", "notification"),
	),
)

var readyKeyboard = tgbotapi.NewInlineKeyboardMarkup(
	tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("да", "yes"),
		tgbotapi.NewInlineKeyboardButtonData("не", "no"),
	),
)

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

	t.Model = model{}

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

// detect last work date before today, if offset != 0 - detect 'last work date minus offset'
func prevWorkDate(offset int) string {
	today := time.Now()
	if today.Weekday() == time.Monday {
		return today.AddDate(0, 0, offset-3).Format("2006-01-02")
	}

	return today.AddDate(0, 0, offset-1).Format("2006-01-02")
}

func (t Texporter) sendTextToChannel(chatID int64, text string) error {
	msg := tgbotapi.NewMessage(chatID, text)

	_, err := t.TelegramBot.Send(msg)
	if err != nil {
		return err
	}

	return nil
}

func (t Texporter) exportTimeEntries(date string, isTest bool) {
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

		timeEntries, err := t.getListTimeEntries(date, p.ID)
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

func newDateKeyboard() tgbotapi.InlineKeyboardMarkup {
	today := time.Now()
	dates := [5]string{
		today.AddDate(0, 0, -1).Format("2006-01-02"),
		today.AddDate(0, 0, -2).Format("2006-01-02"),
		today.AddDate(0, 0, -3).Format("2006-01-02"),
		today.AddDate(0, 0, -4).Format("2006-01-02"),
		today.AddDate(0, 0, -5).Format("2006-01-02"),
	}

	return tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("за вчера", dates[0]),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("за %v", dates[1]), dates[1]),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("за %v", dates[2]), dates[2]),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("за %v", dates[3]), dates[3]),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("за %v", dates[4]), dates[4]),
		),
	)
}

func (t Texporter) botRunAndServe() error {
	updateConfig := tgbotapi.NewUpdate(0)

	updateConfig.Timeout = 30

	updates := t.TelegramBot.GetUpdatesChan(updateConfig)

	for update := range updates {
		if update.Message != nil {
			msg := tgbotapi.NewMessage(update.Message.Chat.ID, "что будем делать, ммм?")

			msg.ReplyMarkup = typesKeyboard

			t.Model.state = "type"

			if _, err := t.TelegramBot.Send(msg); err != nil {
				panic(err)
			}
		} else if update.CallbackQuery != nil {
			callback := tgbotapi.NewCallback(update.CallbackQuery.ID, update.CallbackQuery.Data)
			if _, err := t.TelegramBot.Request(callback); err != nil {
				panic(err)
			}

			switch t.Model.state {
			case "type":
				switch update.CallbackQuery.Data {
				case "export":
					msg := tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID, "а за какой день?")
					msg.ReplyMarkup = newDateKeyboard()

					t.Model.state = "date"
					t.Model.isTest = false

					if _, err := t.TelegramBot.Send(msg); err != nil {
						panic(err)
					}
				}
			case "date":
				t.Model.state = "ready"
				t.Model.date = update.CallbackQuery.Data

				// agregate all project for time entries export
				projectsForExport := make([]string, 0)
				for _, p := range t.ProjectList {
					if p.Export {
						projectsForExport = append(projectsForExport, p.Name)
					}
				}

				msg := tgbotapi.NewMessage(
					update.CallbackQuery.Message.Chat.ID,
					fmt.Sprintf("давай повторим - выгружаю списания на проектах: %v, за %v число", projectsForExport, t.Model.date),
				)

				msg.ReplyMarkup = readyKeyboard

				if _, err := t.TelegramBot.Send(msg); err != nil {
					panic(err)
				}
			case "ready":
				if update.CallbackQuery.Data == "yes" {
					msg := tgbotapi.NewMessage(update.CallbackQuery.Message.Chat.ID, "я закончил!")

					t.Logger.Debug("start test export all entries")
					t.exportTimeEntries(t.Model.date, t.Model.isTest)

					if _, err := t.TelegramBot.Send(msg); err != nil {
						panic(err)
					}
				}

				t.Model = model{}
			}

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
