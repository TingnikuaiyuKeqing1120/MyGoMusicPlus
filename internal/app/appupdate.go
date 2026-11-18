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
	release  *appRelease
	err      string
	install  string // "", "downloading", "downloading:NN", or "failed: …"
	ready    string // the version of an APK saved by the shell
}

// appRelease is what the app's repository offers now.
type appRelease struct {
	Tag    string
	APK    string
	Page   string
	Digest string
	Size   int64
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
			Name   string `json:"name"`
			URL    string `json:"browser_download_url"`
			Digest string `json:"digest"`
			Size   int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(res.Body).Decode(&rel); err != nil {
		return nil, err
	}
	r := &appRelease{Tag: rel.Tag, Page: rel.Page}
	for _, as := range rel.Assets {
		if strings.HasSuffix(strings.ToLower(as.Name), ".apk") {
			r.APK, r.Digest, r.Size = as.URL, as.Digest, as.Size
			break
		}
	}
	return r, nil
}

// downloadApk and reopenApkInstaller are supplied by the Android shell. A
// browser opens the release page instead.
var downloadApk func(appRelease, bool)
var reopenApkInstaller func()

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
				if u.release == nil && !u.hasAPK(rel.Tag) {
					a.toast = Tf("New version %s is out", rel.Tag)
				}
				u.release = rel
			} else {
				u.release = nil
			}
		})
	}()
}

// installAppUpdate reuses a saved copy of this release before asking the
// shell to download it. In a browser, it opens the release page.
func (a *app) installAppUpdate() {
	u := &a.appUpd
	if u.release == nil || u.downloading() {
		return
	}
	if u.hasAPK(u.release.Tag) && reopenApkInstaller != nil {
		a.installDownloadedUpdate()
		return
	}
	if u.release.APK != "" && downloadApk != nil {
		u.install = "downloading"
		downloadApk(*u.release, a.settings.UpdateMirror)
		return
	}
	mygo.Shell.OpenExternal(u.release.Page)
}

func (a *app) installDownloadedUpdate() {
	u := &a.appUpd
	if u.ready != "" && !u.downloading() && reopenApkInstaller != nil {
		u.install = ""
		reopenApkInstaller()
	}
}

func (u *appUpdate) hasAPK(tag string) bool {
	return u.ready != "" && strings.TrimPrefix(u.ready, "v") == strings.TrimPrefix(tag, "v")
}

func (u *appUpdate) downloading() bool {
	return u.install == "downloading" || strings.HasPrefix(u.install, "downloading:")
}

// restoreInstallState also works before a release check has finished. The
// Android shell reads the downloaded version from its saved APK.
func (u *appUpdate) restoreInstallState(data string) {
	var state struct {
		State string `json:"state"`
		Ready string `json:"ready"`
	}
	if json.Unmarshal([]byte(data), &state) == nil {
		u.install, u.ready = state.State, state.Ready
	}
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
	return float32(min(max(n, 0), 100)) / 100, true
}
