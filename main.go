package main

import (
	"github.com/apex/log"
	"github.com/google/go-github/github"
	"github.com/apex/log/handlers/cli"
	"os"
	"strings"
	"fmt"
	"bufio"
	"strconv"
	"net/http"
	"github.com/mitchellh/ioprogress"
	"time"
	"io"
	"runtime"
	"net/url"
	"path"
	"flag"
)

func init() {
	log.SetHandler(cli.Default)
}

const USAGE = `
grab [flags] repo

Flags:
	-r [newname] Rename the local download to [newname]
	-p [newpath] Install the local download to [newpath]
	-d           Perform a dry-run - do not download or install

Example:
	$ grab -p "~/bin/" -r "barg" tobyjoe/grab
`

const ERR_NO_RELEASES = "Error: No tags or releases: %v"

func main() {
	renameFlag := flag.String("r", "", "-r [newname]")
	pathFlag   := flag.String("p", "", "-p [/some/path/]")
	dryRunFlag := flag.Bool("d", false, "-d")
	flag.Usage = func() {
		println(USAGE)
	}

	flag.Parse()
	args := flag.Args()

	log.Infof("Flags: rename(%s), path(%s), dry(%t)", *renameFlag, *pathFlag, *dryRunFlag)
	log.Infof("Args : %s", strings.Join(args, ", "))

	if len(args) < 1 {
		log.Fatal(USAGE)
	}

	project := strings.TrimSpace(args[0])
	if project == "" {
		log.Fatal(USAGE)
	}

	parts := strings.Split(project, "/")
	if len(parts) != 2 {
		log.Fatal(USAGE)
	}

	owner := strings.TrimSpace(parts[0])
	repo := strings.TrimSpace(parts[1])
	if owner == "" || repo == "" {
		log.Fatal(USAGE)
	}

	projectUrl := fmt.Sprintf("github.com/%s/%s", owner, repo)

	client := github.NewClient(nil)

	log.Infof("Grabbing from %s...", project)

	_, resp, err := client.Repositories.Get(owner, repo)
	if err != nil {
		if resp == nil {
			log.Fatalf("Error: Cannot reach %s", projectUrl)
		}

		if resp.StatusCode == 404 {
			log.Fatalf("Error: Project does not exist: %s", projectUrl)
		}else {
			log.Fatalf("Error: Unknown error (%d) reaching: %s", resp.StatusCode, projectUrl)
		}
	}

	release, resp, err := client.Repositories.GetLatestRelease(owner, repo)
	if err != nil {
		if resp.StatusCode == 404 {
			log.Infof("No official releases yet. Checking pre-release tags...")
			tags, _, err := client.Repositories.ListTags(owner, repo, nil)
			if err != nil || len(tags) < 1 {
				log.Fatalf(ERR_NO_RELEASES, err)
			}

			tag := tags[0]
			log.Infof("Latest tag: %s (%s)", *tag.Name, *tag.Commit.SHA)

			release, resp, err = client.Repositories.GetReleaseByTag(owner, repo, *tag.Name)
			if err != nil || len(tags) < 1 {
				log.Fatalf(ERR_NO_RELEASES, err)
			}
		}
	}

	assets := release.Assets
	var asset github.ReleaseAsset

	switch(len(assets)) {
	case 0:
		log.Fatal("Error: No assets to download")
		break;
	case 1:
		asset = assets[0]
		break;
	default:
		log.Info("Release Assets:")
		for i, a := range assets {
			log.Infof(" (%d) %s (%s)", i+1, *a.Name, *a.BrowserDownloadURL)
		}

		reader := bufio.NewReader(os.Stdin)
		fmt.Print("Which asset would you like to download? ")
		choice, _ := reader.ReadString('\n')
		choice = strings.TrimSpace(choice)
		choiceInt, err := strconv.Atoi(choice)
		if err != nil {
			log.Fatalf("Error: You must select an asset to download")
		}
		println("")
		asset = assets[choiceInt-1]

		break;
	}

	if *dryRunFlag {
		log.Info("Dry-run completed")
	}else {
		downloadReleaseAsset(&asset, renameFlag, pathFlag)
	}
}

func downloadReleaseAsset(r *github.ReleaseAsset, newName *string, newPath *string) {
	var outDir string
	if newPath == nil {
		outDir = binDir()
	}else{
		outDir = *newPath
	}

	downloadUrlString := *r.BrowserDownloadURL

	log.Infof("Downloading release asset: %s", downloadUrlString)
	log.Infof("Copying to: %s", outDir)

	downloadUrl, err := url.Parse(downloadUrlString)
	if err != nil {
		log.Fatalf("Error: Could not parse URL: %v", err)
	}

	var fileName string
	if newName == nil {
		_, fileName = path.Split(downloadUrl.Path)
	}else{
		fileName = *newName
	}

	outName := outDir + "/" + fileName
	out, err := os.Create(outName)
	if err != nil {
		log.Fatalf("Error: Could not create local file: %v", err)
	}

	defer out.Close()
	resp, err := http.Get(downloadUrlString)
	if err != nil {
		log.Fatalf("Error: Could not get remote file: %v", err)
	}
	defer resp.Body.Close()
	bar := ioprogress.DrawTextFormatBar(20)
	progressFunc := ioprogress.DrawTerminalf(os.Stdout, func(progress, total int64) string {
		return fmt.Sprintf("%s %s %20s", fileName, bar(progress, total), ioprogress.DrawTextFormatBytes(progress, total))
	})

	progress := &ioprogress.Reader{
		Reader:       resp.Body,
		Size:         resp.ContentLength,
		DrawInterval: time.Millisecond,
		DrawFunc:     progressFunc,
	}

	_, err = io.Copy(out, progress)
	if err != nil {
		log.Fatalf("Error: Could not copy local file: %v", err)
	}

	err = os.Chmod(outName, 0755)
	if err != nil {
		log.Fatalf("Error: Could not make %s executable. Try with (sudo)?", outName)
	}
}

func binDir() string {
	dir, err := os.Getwd()
	if err != nil {
		log.Fatalf("Error: No working directory")
	}
	log.Infof("GOOS: %s", runtime.GOOS)

	switch(runtime.GOOS) {
	case "darwin":
		fallthrough
	case "dragonfly":
		fallthrough
	case "linux":
		fallthrough
	case "freebsd":
		fallthrough
	case "netbsd":
		fallthrough
	case "openbsd":
		dir = "/usr/local/bin"
		break
	default:
		break
	}
	return dir
}

