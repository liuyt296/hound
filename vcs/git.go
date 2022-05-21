package vcs

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const defaultRef = "master"

var headBranchRegexp = regexp.MustCompile(`HEAD branch: (?P<branch>.+)`)
var autoGeneratedFileRegexp = regexp.MustCompile(`(?P<path>.+) linguist-generated=true`)

func init() {
	Register(newGit, "git")
}

type GitDriver struct {
	DetectRef     bool   `json:"detect-ref"`
	Ref           string `json:"ref"`
	refDetetector refDetetector
}

type refDetetector interface {
	detectRef(dir string) string
}

type headBranchDetector struct {
}

func newGit(b []byte) (Driver, error) {
	var d GitDriver

	if b != nil {
		if err := json.Unmarshal(b, &d); err != nil {
			return nil, err
		}
	}

	d.refDetetector = &headBranchDetector{}

	return &d, nil
}

func (g *GitDriver) HeadRev(dir string) (string, error) {
	cmd := exec.Command(
		"git",
		"rev-parse",
		"HEAD")
	cmd.Dir = dir
	r, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	defer r.Close()

	if err := cmd.Start(); err != nil {
		return "", err
	}

	var buf bytes.Buffer

	if _, err := io.Copy(&buf, r); err != nil {
		return "", err
	}

	return strings.TrimSpace(buf.String()), cmd.Wait()
}

func run(desc, dir, cmd string, args ...string) (string, error) {
	c := exec.Command(cmd, args...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		log.Printf(
			"Failed to %s %s, see output below\n%sContinuing...",
			desc,
			dir,
			out)
	}

	return string(out), nil
}

func (g *GitDriver) Pull(dir string) (string, error) {
	targetRef := g.targetRef(dir)

	if _, err := run("git fetch", dir,
		"git",
		"fetch",
		"--prune",
		"--no-tags",
		"--depth", "1",
		"origin",
		fmt.Sprintf("+%s:remotes/origin/%s", targetRef, targetRef)); err != nil {
		return "", err
	}

	if _, err := run("git reset", dir,
		"git",
		"reset",
		"--hard",
		fmt.Sprintf("origin/%s", targetRef)); err != nil {
		return "", err
	}

	return g.HeadRev(dir)
}

func (g *GitDriver) targetRef(dir string) string {
	var targetRef string
	if g.Ref != "" {
		targetRef = g.Ref
	} else if g.DetectRef {
		targetRef = g.refDetetector.detectRef(dir)
	}

	if targetRef == "" {
		targetRef = defaultRef
	}

	return targetRef
}

func (g *GitDriver) Clone(dir, url string) (string, error) {
	par, rep := filepath.Split(dir)
	cmd := exec.Command(
		"git",
		"clone",
		"--depth", "1",
		url,
		rep)
	cmd.Dir = par
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Failed to clone %s, see output below\n%sContinuing...", url, out)
		return "", err
	}

	return g.Pull(dir)
}

func (g *GitDriver) SpecialFiles() []string {
	return []string{
		".git",
	}
}

func (g *GitDriver) AutoGeneratedFilePatterns(dir string) []string {
	var filePatterns []string
	path := filepath.Join(dir, ".gitattributes")

	file, err := os.Open(path)
	if err != nil {
		return filePatterns
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		matches := autoGeneratedFileRegexp.FindStringSubmatch(scanner.Text())
		if len(matches) == 2 {
			pattern := strings.ReplaceAll(matches[1], "**", "*")
			pattern = strings.ReplaceAll(pattern, "*", ".*")
			filePatterns = append(filePatterns, pattern)
		}
	}

	return filePatterns
}

func (d *headBranchDetector) detectRef(dir string) string {
	output, err := run("git show remote info", dir,
		"git",
		"remote",
		"show",
		"origin",
	)

	if err != nil {
		log.Printf(
			"error occured when fetching info to determine target ref in %s: %s. Will fall back to default ref %s",
			dir,
			err,
			defaultRef,
		)
		return ""
	}

	matches := headBranchRegexp.FindStringSubmatch(output)
	if len(matches) != 2 {
		log.Printf(
			"could not determine target ref in %s. Will fall back to default ref %s",
			dir,
			defaultRef,
		)
		return ""
	}

	return matches[1]
}
