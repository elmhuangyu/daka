package notify

import (
	"fmt"
	"strings"
	"time"
)

// Languages the bot messages are available in. The empty string means English.
const (
	LangEN   = "en"
	LangZhCN = "zh-CN"
)

// ValidLang reports whether lang is a supported message language ("" selects English).
func ValidLang(lang string) bool {
	return lang == "" || lang == LangEN || lang == LangZhCN
}

type catalog struct {
	private         string
	nothingDue      string
	dueToday        string // "%s (%d):"-style title only
	maintenance     string
	more            string
	overdueByItem   string // %s name, %d days
	reminder        string // %s name
	digestHeader    func(local time.Time) string
	overdueInList   string // %s name, %d days
	test            string
	linked          string // %s optional " at HH:MM"
	linkedSuffix    string
	chatTaken       string
	invalidCode     string
	sectionTemplate string // %s title, %d count
}

var zhWeekdays = [...]string{"星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"}

var catalogs = map[string]catalog{
	LangEN: {
		private:       "a private item",
		nothingDue:    "Nothing due today.",
		dueToday:      "Due today",
		maintenance:   "Maintenance overdue",
		more:          "- ...and more\n",
		overdueByItem: "Reminder: %s is overdue by %d day(s).",
		reminder:      "Reminder: %s",
		digestHeader: func(l time.Time) string {
			return fmt.Sprintf("Daka - %s\n", l.Format("Monday 2006-01-02"))
		},
		overdueInList:   "- %s (%d day(s) overdue)\n",
		test:            "Hello from Daka! Your Telegram bot is working.",
		linked:          "Linked to Daka. You will get a daily digest%s (your Daka time zone).",
		linkedSuffix:    " at %s",
		chatTaken:       "This chat is already linked to a Daka account.",
		invalidCode:     "That link code is invalid or expired. Create a new one in Daka settings.",
		sectionTemplate: "\n%s (%d):\n",
	},
	LangZhCN: {
		private:       "一个私密事项",
		nothingDue:    "今天没有待办。",
		dueToday:      "今日待办",
		maintenance:   "维护已逾期",
		more:          "- ……还有更多\n",
		overdueByItem: "提醒：%s 已逾期 %d 天。",
		reminder:      "提醒：%s",
		digestHeader: func(l time.Time) string {
			return fmt.Sprintf("Daka - %s %s\n", l.Format("2006-01-02"), zhWeekdays[l.Weekday()])
		},
		overdueInList:   "- %s（已逾期 %d 天）\n",
		test:            "来自 Daka 的问候！你的 Telegram 机器人工作正常。",
		linked:          "已关联 Daka，你将收到每日摘要%s（按你在 Daka 中设置的时区）。",
		linkedSuffix:    "，发送时间 %s",
		chatTaken:       "此聊天已关联到另一个 Daka 账号。",
		invalidCode:     "关联码无效或已过期，请在 Daka 设置中重新生成。",
		sectionTemplate: "\n%s（%d）：\n",
	},
}

func msgsFor(lang string) catalog {
	if c, ok := catalogs[lang]; ok {
		return c
	}
	return catalogs[LangEN]
}

func (c catalog) linkedText(digestTime string) string {
	extra := ""
	if digestTime != "" {
		extra = fmt.Sprintf(c.linkedSuffix, digestTime)
	}
	return fmt.Sprintf(c.linked, extra)
}

func (c catalog) itemName(e entry) string {
	if e.Sensitive {
		return c.private
	}
	return e.Name
}

func (c catalog) itemText(e entry) string {
	if e.OverdueDays > 0 {
		return fmt.Sprintf(c.overdueByItem, c.itemName(e), e.OverdueDays)
	}
	return fmt.Sprintf(c.reminder, c.itemName(e))
}

func (c catalog) digestText(local time.Time, pending []entry) string {
	var b strings.Builder
	b.WriteString(c.digestHeader(local))
	var due, overdue []entry
	for _, e := range pending {
		if e.OverdueDays > 0 {
			overdue = append(overdue, e)
		} else {
			due = append(due, e)
		}
	}
	if len(pending) == 0 {
		b.WriteString(c.nothingDue)
		return b.String()
	}
	listed := 0
	section := func(title string, es []entry) {
		if len(es) == 0 {
			return
		}
		fmt.Fprintf(&b, c.sectionTemplate, title, len(es))
		for _, e := range es {
			if listed >= maxListed || b.Len() > 3500 {
				b.WriteString(c.more)
				return
			}
			listed++
			if e.OverdueDays > 0 {
				fmt.Fprintf(&b, c.overdueInList, c.itemName(e), e.OverdueDays)
			} else {
				fmt.Fprintf(&b, "- %s\n", c.itemName(e))
			}
		}
	}
	section(c.dueToday, due)
	section(c.maintenance, overdue)
	return strings.TrimRight(b.String(), "\n")
}
