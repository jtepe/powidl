package pod

import (
	"archive/zip"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

const (
	ReservedPodName = "all"
	feedFileName    = "feed.zip"
)

// Podcast represents a podcast. It has a feed URL, name
// and additional metadata.
type Podcast struct {
	FeedURL    string `json:"feed_url"`    // URL to retrieve the podcast feed from
	Name       string `json:"name"`        // The name under which this podcast is managed
	LocalStore string `json:"local_store"` // Directory path of the local store for this podcast
}

// New creates a new podcast and intializes the
// local storage for it. If creation of the local storage
// fails, or a podcast by that name is already managed by
// gopodgrab, an error is returned.
// If the refresh of the feed, or adding the configuration
// of the podcast fails, an error is returned, as well.
func New(name, feedURL, storageDir string) (*Podcast, error) {
	if name == ReservedPodName {
		return nil, ErrReservedName
	}

	if podExists(name) {
		return nil, ErrPodExists
	}

	pod := &Podcast{
		Name:       name,
		FeedURL:    feedURL,
		LocalStore: storageDir,
	}

	if err := pod.refreshFeed(); err != nil {
		return nil, err
	}

	if err := addPod(pod); err != nil {
		return nil, err
	}

	return pod, nil
}

// List returns the list of managed podcasts from
// the configuration file.
// Failure to read the configuration file results in a error.
func List() ([]*Podcast, error) {
	pods, err := readPods()
	if err != nil {
		return nil, err
	}

	res := make([]*Podcast, 0, len(pods))
	for _, p := range pods {
		res = append(res, p)
	}

	return res, nil
}

// Get returns a specific podcast from the configuration by name.
// If the podcast is not found by name, or the configuration file
// cannot be read, then an error is returned.
func Get(name string) (*Podcast, error) {
	pods, err := readPods()
	if err != nil {
		return nil, err
	}

	pod, ok := pods[name]
	if !ok {
		return nil, ErrNoEntry
	}

	return pod, nil
}

// refreshFeed updates the locally stored feed from remote.
func (pod *Podcast) refreshFeed() error {
	resp, err := http.Get(pod.FeedURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	err = pod.storeExists()
	if err != nil {
		return err
	}

	f, err := os.Create(pod.feedFile())
	if err != nil {
		return err
	}
	defer f.Close()

	zipper := zip.NewWriter(f)

	file, err := zipper.Create(pod.Name)
	if err != nil {
		return err
	}

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return err
	}

	err = zipper.Close()
	if err != nil {
		return err
	}

	return nil
}

// NewEpisodes reads the feed and compares the list of episodes in
// the feed against the one already in the local storage.
// It returns the difference feed - storage.
func (pod *Podcast) NewEpisodes() ([]*Episode, error) {
	stored, err := pod.readStore()
	if err != nil {
		return nil, err
	}

	arc, err := zip.OpenReader(pod.feedFile())
	if err != nil {
		return nil, err
	}

	defer arc.Close()

	if len(arc.File) < 1 {
		return nil, ErrArchiveEmpty
	}

	feed, err := arc.File[0].Open()
	if err != nil {
		return nil, err
	}

	defer feed.Close()

	feedEpis, err := parseFeed(feed)
	if err != nil {
		return nil, err
	}

	var newEpis []*Episode
	for _, e := range feedEpis {
		if !stored[e.Title] {
			newEpis = append(newEpis, e)
		}
	}

	return newEpis, nil
}

// readStore reads the list of episodes that are in the local
// storage of the podcast returning a set of filenames without
// extensions.
func (pod *Podcast) readStore() (map[string]bool, error) {
	dir, err := os.OpenFile(pod.LocalStore, os.O_RDONLY, os.ModeDir)
	if err != nil {
		return nil, err
	}

	content, err := dir.Readdirnames(0)
	if err != nil {
		return nil, err
	}

	stored := make(map[string]bool, len(content))

	for _, e := range content {
		if e == feedFileName {
			continue
		}

		e = strings.TrimSuffix(e, filepath.Ext(e))
		stored[e] = true
	}

	return stored, nil
}

// storeExists ensures that the podcast storage directory is present.
func (pod *Podcast) storeExists() error {
	if err := os.MkdirAll(pod.LocalStore, os.ModeDir|0755); err != nil {
		return err
	}

	return nil
}

// feedFile returns the full file path of the locally stored, zipped feed.
func (pod *Podcast) feedFile() string {
	return filepath.Join(pod.LocalStore, feedFileName)
}

// dirExists checks whether the directory specified by path exists.
func dirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	return info.IsDir()
}

// DownloadEpisodes retrieves all episodes and stores them in the local
// storage. For each retrieved episode the size in bytes is recorded
// in Episode.Bytes.
func (pod *Podcast) DownloadEpisodes(eps []*Episode) error {
	for _, e := range eps {

		if err := download(e, pod.LocalStore); err != nil {
			return err
		}
	}

	return nil
}

// download downloads Episode e to the directory dir.
func download(e *Episode, dir string) error {
	u, err := url.Parse(e.File.URL)
	if err != nil {
		return err
	}

	resp, err := http.Get(u.String())
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	ext := filepath.Ext(u.Path)

	f, err := os.Create(filepath.Join(dir, e.Title+ext))
	if err != nil {
		return err
	}
	defer f.Close()

	n, err := io.Copy(f, resp.Body)
	if err != nil {
		return err
	}
	e.Bytes = n

	return nil
}
