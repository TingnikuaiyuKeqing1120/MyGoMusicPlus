package app

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"mygo-music/internal/plugin"
)

// plusRepo is where this app's own releases are published, for the web and
// the Android shell to check for a new version: the desktop has the update
// plugin instead.
const plusRepo = "TingnikuaiyuKeqing1120/MyGoMusicPlus"

// appCheckEvery is how often the app's releases are looked at.
const appCheckEvery = 24 * time.Hour

// appUpdate is what checking for a new version of the app came to.
type appUpdate struct {
	checking bool
	version  string // the newest release's tag, while it's newer than this
	apk      string // the release's APK, for the shell to install
	page     string // the release's page, for a browser to open
	err      string
	install  string // "", "downloading", "installing", or "failed: …"
}

// appRelease is what the app's repository offers now.
type appRelease struct {
	Tag  string
	APK  string
	Page string
}

// fetchAppRelease asks GitHub what the latest release is; tests replace it.
var fetchAppRelease = func() (*appRelease, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	res, err := client.Get("https://api.github.com/repos/" + plusRepo + "/releases/latest")
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, errors.New(res.Status)
	}
	var rel struct {
		Tag    string `json:"tag_name"`
		Page   string `json:"html_url"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(res.Body).Decode(&rel); err != nil {
		return nil, err
	}
	r := &appRelease{Tag: rel.Tag, Page: rel.Page}
	for _, as := range rel.Assets {
		if strings.HasSuffix(strings.ToLower(as.Name), ".apk") {
			r.APK = as.URL
			break
		}
	}
	return r, nil
}

// installApk hands the APK to the shell's system installer; nil where no
// shell offers it, the release's page opening instead. Tests replace it.
var installApk func(url string)

// keepCheckingAppUpdate looks at the app's releases now if it's time, and
// from then on as the day comes round, for as long as the app runs.
func (a *app) keepCheckingAppUpdate() {
	a.checkAppUpdateDaily()
	time.AfterFunc(appCheckEvery, func() { a.post(a.keepCheckingAppUpdate) })
}

// checkAppUpdateDaily checks unless it's been done in the last day.
func (a *app) checkAppUpdateDaily() {
	if !a.settings.AppChecked.IsZero() && time.Since(a.settings.AppChecked) < appCheckEvery {
		return
	}
	a.settings.AppChecked = time.Now()
	a.settings.save()
	a.checkAppUpdate()
}

// checkAppUpdate asks what the latest release is, and keeps it if it's newer
// than what's running.
func (a *app) checkAppUpdate() {
	u := &a.appUpd
	if u.checking {
		return
	}
	u.checking, u.err = true, ""
	go func() {
		rel, err := fetchAppRelease()
		a.post(func() {
			u.checking = false
			if err != nil {
				u.err = err.Error()
				return
			}
			if plugin.IsNewer(rel.Tag, mygo.App.Version()) {
				if u.version == "" {
					a.toast = Tf("New version %s is out", rel.Tag)
				}
				u.version, u.apk, u.page = rel.Tag, rel.APK, rel.Page
			} else {
				u.version, u.apk, u.page = "", "", ""
			}
		})
	}()
}

// installAppUpdate takes the new version in: the shell's system installer
// where there's one, the release's page in a browser otherwise.
func (a *app) installAppUpdate() {
	u := &a.appUpd
	if u.apk != "" && installApk != nil {
		u.install = "downloading"
		installApk(u.apk)
		return
	}
	mygo.Shell.OpenExternal(u.page)
}

// downloadProgress is how far the update's download has come, and whether
// its size is known: the shell says "downloading:NN" as the percent comes.
func (u *appUpdate) downloadProgress() (frac float32, known bool) {
	pct, ok := strings.CutPrefix(u.install, "downloading:")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(pct)
	if err != nil {
		return 0, false
	}
	return float32(min(n, 100)) / 100, true
}