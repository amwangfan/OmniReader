package reading

import "time"

type DeviceInput struct {
	ID           string `json:"id"`
	DisplayName  string `json:"displayName"`
	SystemName   string `json:"systemName"`
	Platform     string `json:"platform"`
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	AppVersion   string `json:"appVersion"`
}

type Device struct {
	ID           string     `json:"id"`
	DisplayName  string     `json:"displayName"`
	SystemName   string     `json:"systemName"`
	Platform     string     `json:"platform"`
	Manufacturer string     `json:"manufacturer"`
	Model        string     `json:"model"`
	AppVersion   string     `json:"appVersion"`
	LastSeenAt   time.Time  `json:"lastSeenAt"`
	DisabledAt   *time.Time `json:"disabledAt"`
}

type DeviceSummary struct {
	Device
	LatestBook          *BookProgressSummary `json:"latestBook"`
	TodayReadSeconds    int64                `json:"todayReadSeconds"`
	SevenDayReadSeconds int64                `json:"sevenDayReadSeconds"`
	TotalReadSeconds    int64                `json:"totalReadSeconds"`
}

type DeviceDetail struct {
	Device
	Books               []BookProgressSummary `json:"books"`
	TodayReadSeconds    int64                 `json:"todayReadSeconds"`
	SevenDayReadSeconds int64                 `json:"sevenDayReadSeconds"`
	TotalReadSeconds    int64                 `json:"totalReadSeconds"`
}

type Locator struct {
	Version         int     `json:"version"`
	ContentRevision string  `json:"contentRevision"`
	ChapterHref     string  `json:"chapterHref"`
	ChapterIndex    int     `json:"chapterIndex"`
	BlockIndex      int     `json:"blockIndex"`
	CharOffset      int     `json:"charOffset"`
	TextQuote       string  `json:"textQuote"`
	TextHash        string  `json:"textHash"`
	ChapterProgress float64 `json:"chapterProgress"`
	BookProgress    float64 `json:"bookProgress"`
}

type ProgressInput struct {
	BookID           string           `json:"-"`
	DeviceID         string           `json:"deviceId"`
	Locator          Locator          `json:"locator"`
	Percentage       *float64         `json:"percentage"`
	ClientUpdatedAt  *time.Time       `json:"clientUpdatedAt"`
	DailyReadSeconds map[string]int64 `json:"dailyReadSeconds"`
}

type Progress struct {
	BookID           string     `json:"bookId"`
	DeviceID         string     `json:"deviceId"`
	DeviceName       string     `json:"deviceName"`
	Locator          Locator    `json:"locator"`
	Percentage       *float64   `json:"percentage"`
	ClientUpdatedAt  *time.Time `json:"clientUpdatedAt,omitempty"`
	UpdatedAt        time.Time  `json:"updatedAt"`
	RevisionMismatch bool       `json:"revisionMismatch"`
}

type ProgressResult struct {
	Device          *Progress `json:"deviceProgress"`
	Global          *Progress `json:"globalProgress"`
	ContentRevision string    `json:"contentRevision"`
}

type BookProgressSummary struct {
	BookID           string     `json:"bookId"`
	Title            string     `json:"title"`
	Locator          Locator    `json:"locator"`
	Percentage       *float64   `json:"percentage"`
	ClientUpdatedAt  *time.Time `json:"clientUpdatedAt,omitempty"`
	UpdatedAt        time.Time  `json:"updatedAt"`
	RevisionMismatch bool       `json:"revisionMismatch"`
	ReadSeconds      int64      `json:"readSeconds"`
}

type DailyActivity struct {
	ReadingDate string `json:"readingDate"`
	ReadSeconds int64  `json:"readSeconds"`
}

type Activity struct {
	DeviceID         string          `json:"deviceId"`
	From             string          `json:"from"`
	To               string          `json:"to"`
	Days             []DailyActivity `json:"days"`
	TotalReadSeconds int64           `json:"totalReadSeconds"`
}

type Dashboard struct {
	Devices             []DeviceSummary `json:"devices"`
	DeviceCount         int             `json:"deviceCount"`
	TodayReadSeconds    int64           `json:"todayReadSeconds"`
	SevenDayReadSeconds int64           `json:"sevenDayReadSeconds"`
	TotalReadSeconds    int64           `json:"totalReadSeconds"`
	Global              *Progress       `json:"globalProgress"`
	GlobalBookTitle     string          `json:"globalBookTitle"`
}
