package internal

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	goversion "github.com/hashicorp/go-version"
	"github.com/hashicorp/hc-install/product"
	"github.com/hashicorp/terraform-config-inspect/tfconfig"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

var home string
var terraformLinuxBinariesPath string

func init() {
	var err error
	home, err = os.UserHomeDir()
	if err != nil {
		panic(err)
	}

	terraformLinuxBinariesPath = filepath.Join(home, ".tfbox", "linux")
}

func Run(rootDirectory, workingDirectory, tfVersion string, tfArgs []string, showLogs bool) error {
	ctx := context.Background()

	if rootDirectory == "" {
		pwd, err := os.Getwd()
		if err != nil {
			return err
		}
		rootDirectory = pwd
	} else {
		var err error
		rootDirectory, err = absPath(rootDirectory)
		if err != nil {
			return err
		}
	}

	var err error

	if workingDirectory == "" {
		workingDirectory = "."
	}

	if tfVersion == "" {
		tfVersion, err = getTfVersionFromConfig(filepath.Join(rootDirectory, workingDirectory))
		if err != nil {
			return err
		}
	}

	terraformBinaryPath, err := downloadTerraformBinary(ctx, tfVersion)
	if err != nil {
		return err
	}

	imageName := "mcr.microsoft.com/devcontainers/base:alpine"

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}

	imageAvailable, err := isImageAvailableLocally(ctx, cli, imageName)
	if err != nil {
		return err
	}

	if !imageAvailable {
		err = pullImage(ctx, cli, imageName, showLogs)
		if err != nil {
			return err
		}
	} else {
		//fmt.Printf("Image %s is already available locally. No need to pull.\n", imageName)
	}

	environmentVariablesMap := make(map[string]string)
	for _, env := range os.Environ() {
		split := strings.Split(env, "=")
		environmentVariablesMap[split[0]] = split[1]
	}

	// Set/Overwrite custom environment variables
	environmentVariablesMap["AWS_CONFIG_FILE"] = "/.aws/config"
	environmentVariablesMap["AWS_SHARED_CREDENTIALS_FILE"] = "/.aws/credentials"
	environmentVariablesMap["TMPDIR"] = "/tmp"

	environmentVariableList := make([]string, 0, len(environmentVariablesMap))
	for key, value := range environmentVariablesMap {
		environmentVariableList = append(environmentVariableList, fmt.Sprintf("%s=%s", key, value))
	}

	containerConfig := &container.Config{
		Image:      imageName,
		WorkingDir: fmt.Sprintf("/host/%s", workingDirectory),
		Env:        environmentVariableList,
		Cmd:        append([]string{"terraform"}, tfArgs...),
		//Entrypoint:   []string{"tail", "-f", "/dev/null"},
		AttachStdout: true,
		AttachStdin:  true,
	}

	hostConfig := &container.HostConfig{
		Binds: []string{
			fmt.Sprintf("%s:/host", rootDirectory),
			fmt.Sprintf("%s/.aws:/.aws", home),
			fmt.Sprintf("%s:/usr/local/bin/%s", terraformBinaryPath, product.Terraform.BinaryName()),
			fmt.Sprintf("%s/.netrc:/root/.netrc", home),
		},
	}

	containerResp, err := cli.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, "")
	if err != nil {
		return err
	}

	if err = cli.ContainerStart(ctx, containerResp.ID, container.StartOptions{}); err != nil {
		return err
	}

	defer func() {
		err = cli.ContainerRemove(ctx, containerResp.ID, container.RemoveOptions{Force: true})
		if err != nil {
			log.Println(err)
		}
	}()

	done := make(chan error, 1)       // Add buffer
	outputDone := make(chan error, 1) // Add buffer

	// Start the log processing goroutine
	go func() {
		out, err := cli.ContainerLogs(ctx, containerResp.ID, container.LogsOptions{
			ShowStdout: true,
			ShowStderr: true,
			Follow:     true,
		})
		if err != nil {
			outputDone <- fmt.Errorf("error fetching container logs: %w", err)
			return
		}
		defer func(out io.ReadCloser) {
			err = out.Close()
			if err != nil {
				log.Println(err)
			}
		}(out)

		if !showLogs {
			_, err = stdcopy.StdCopy(io.Discard, io.Discard, out)
		} else {
			_, err = stdcopy.StdCopy(os.Stdout, os.Stderr, out)
		}
		outputDone <- err
	}()

	// Start the container wait goroutine
	go func() {
		statusCh, errCh := cli.ContainerWait(ctx, containerResp.ID, container.WaitConditionNotRunning)
		select {
		case status := <-statusCh:
			if status.Error != nil {
				done <- fmt.Errorf("container error: %s", status.Error.Message)
			} else {
				done <- nil
			}
		case err := <-errCh:
			done <- fmt.Errorf("error waiting for container: %w", err)
		}
	}()

	// Wait for both goroutines to complete
	var outputErr, waitErr error
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		outputErr = <-outputDone
	}()

	go func() {
		defer wg.Done()
		waitErr = <-done
	}()

	wg.Wait()

	// Return the first non-nil error, or nil if both succeeded
	if outputErr != nil {
		return outputErr
	}
	return waitErr
}

func downloadTerraformBinary(ctx context.Context, tfVersion string) (string, error) {
	binaryDirPath := filepath.Join(terraformLinuxBinariesPath, tfVersion)
	binaryFilePath := filepath.Join(binaryDirPath, product.Terraform.BinaryName())
	_, err := os.Stat(binaryFilePath)
	if err == nil {
		return binaryFilePath, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	err = os.MkdirAll(binaryDirPath, os.ModePerm)
	if err != nil {
		return "", err
	}

	arch := mapTerraformArch(runtime.GOARCH)
	if arch == "" {
		return "", fmt.Errorf("Unsupported architecture: %s\n", runtime.GOARCH)
	}

	downloadURL := fmt.Sprintf("https://releases.hashicorp.com/terraform/%s/terraform_%s_linux_%s.zip", tfVersion, tfVersion, arch)
	tempDir, err := os.MkdirTemp("", "tfinstaller")
	if err != nil {
		return "", err
	}
	defer func(p string) {
		err = os.RemoveAll(p)
		if err != nil {
			log.Println(err)
		}
	}(tempDir)

	zipPath, err := downloadFile(downloadURL, tempDir)
	if err != nil {
		return "", fmt.Errorf("Error downloading file: %v\n", err)
	}

	err = unzipFile(zipPath, binaryDirPath)
	if err != nil {
		return "", fmt.Errorf("Error unzipping file: %v\n", err)
	}

	err = os.Chmod(binaryFilePath, 0755)
	if err != nil {
		return "", err
	}

	return binaryFilePath, nil
}

func isImageAvailableLocally(ctx context.Context, cli *client.Client, imageName string) (bool, error) {
	imageList, err := cli.ImageList(ctx, image.ListOptions{})
	if err != nil {
		return false, err
	}

	for _, img := range imageList {
		for _, tag := range img.RepoTags {
			if tag == imageName {
				return true, nil
			}
		}
	}
	return false, nil
}

func getTfVersionFromConfig(projectPath string) (string, error) {
	module, diags := tfconfig.LoadModule(projectPath)
	if (diags != nil && diags.HasErrors()) || module == nil {
		return getTfVersionBasedOnConstraint("")
	}

	return getTfVersionBasedOnConstraint(strings.Join(module.RequiredCore, ","))
}

func getTfVersionBasedOnConstraint(constraint string) (string, error) {
	terraformReleasesURL := "https://releases.hashicorp.com/terraform/"
	resp, err := http.Get(terraformReleasesURL)
	if err != nil {
		return "", err
	}
	defer func(Body io.ReadCloser) {
		err = Body.Close()
		if err != nil {
			log.Println(err)
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get Terraform versions page: %v", resp.Status)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return "", err
	}

	var versions []string
	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		href, exists := s.Attr("href")
		if exists && strings.HasPrefix(href, "/terraform/") {
			version := strings.TrimPrefix(href, "/terraform/")
			version = strings.TrimSuffix(version, "/")
			if version != "" {
				versions = append(versions, version)
			}
		}
	})

	versionConstraints, err := goversion.NewConstraint(constraint)
	if err != nil {
		versionConstraints = nil
	}

	for _, version := range versions {
		semver, err := goversion.NewSemver(version)
		if err != nil {
			log.Println(err)
			continue
		}

		if semver.Prerelease() == "" {
			if len(versionConstraints) == 0 {
				return version, nil
			} else {
				match := true
				for _, c := range versionConstraints {
					if !c.Check(semver) {
						match = false
					}
				}

				if match {
					return version, nil
				}
			}
		}
	}

	return "", fmt.Errorf("couldn't find a matching version")
}

func pullImage(ctx context.Context, cli *client.Client, imageName string, showLogs bool) error {
	if showLogs {
		fmt.Printf("Image %s not found locally. Pulling from registry...\n", imageName)
	}

	reader, err := cli.ImagePull(ctx, imageName, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("failed to pull image: %w", err)
	}
	defer func() {
		if closeErr := reader.Close(); closeErr != nil {
			log.Printf("failed to close reader: %v", closeErr)
		}
	}()

	maxLines := 10
	logBuffer := make([]string, 0, maxLines+1)

	// Decode the JSON stream
	decoder := json.NewDecoder(reader)

	for {
		var progress struct {
			Status         string `json:"status"`
			ID             string `json:"id,omitempty"`
			Progress       string `json:"progress,omitempty"`
			ProgressDetail struct {
				Current int `json:"current"`
				Total   int `json:"total"`
			} `json:"progressDetail"`
		}

		if err = decoder.Decode(&progress); err == io.EOF {
			break // End of stream
		} else if err != nil {
			return fmt.Errorf("error decoding image pull progress: %w", err)
		}

		var l string
		if showLogs {
			if progress.ID != "" {
				l = fmt.Sprintf("[Image %s] %s: %s", progress.ID, progress.Status, progress.Progress)
			} else {
				l = fmt.Sprintf("[Status] %s", progress.Status)
			}
		}

		logBuffer = append(logBuffer, l)
		if len(logBuffer) > maxLines {
			logBuffer = logBuffer[1:]
		}

		if showLogs {
			dynamicLogPrint(logBuffer, maxLines)
		}
	}

	if showLogs {
		fmt.Printf("Pull completed: %s\n", imageName)
	}

	return nil
}

func mapTerraformArch(goarch string) string {
	switch goarch {
	case "amd64":
		return "amd64"
	case "arm64":
		return "arm64"
	case "386":
		return "386"
	default:
		return ""
	}
}

func downloadFile(url, target string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP request: %w", err)
	}
	defer func(Body io.ReadCloser) {
		err = Body.Close()
		if err != nil {
			log.Println(err)
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("bad status code: %d", resp.StatusCode)
	}

	dest := filepath.Join(target, filepath.Base(url))
	outFile, err := os.Create(dest)
	if err != nil {
		return "", fmt.Errorf("failed to create destination file: %w", err)
	}
	defer func(outFile *os.File) {
		err = outFile.Close()
		if err != nil {
			log.Println(err)
		}
	}(outFile)

	// Copy content to the file
	_, err = io.Copy(outFile, resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to copy content: %w", err)
	}
	return dest, nil
}

func unzipFile(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return fmt.Errorf("failed to open zip file: %w", err)
	}
	defer func(r *zip.ReadCloser) {
		err = r.Close()
		if err != nil {
			log.Println(err)
		}
	}(r)

	if err = os.MkdirAll(destDir, os.ModePerm); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	for _, file := range r.File {
		path := filepath.Join(destDir, file.Name)
		// Check for zip slip vulnerability (secure extraction)
		if !strings.HasPrefix(filepath.Clean(path), filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path: %s", path)
		}

		if file.FileInfo().IsDir() {
			if err = os.MkdirAll(path, os.ModePerm); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
			continue
		}

		// Extract the file
		if err = extractFile(file, path); err != nil {
			return fmt.Errorf("failed to extract file: %w", err)
		}
	}
	return nil
}

func extractFile(file *zip.File, destPath string) error {
	src, err := file.Open()
	if err != nil {
		return fmt.Errorf("failed to open file in zip: %w", err)
	}
	defer func(src io.ReadCloser) {
		err = src.Close()
		if err != nil {
			log.Println(err)
		}
	}(src)

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer func(out *os.File) {
		err = out.Close()
		if err != nil {
			log.Println(err)
		}
	}(out)

	_, err = io.Copy(out, src)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}
	return nil
}
