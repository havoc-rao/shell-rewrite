// Package cli: shr update — 从 GitHub Releases 自更新。
//
// 逻辑等价于 scripts/install.sh，但仅用 Go 标准库实现，不引入任何外部依赖：
//  1. 解析最新 release tag（优先走 releases/latest 重定向，不受 api 限流影响；
//     失败回退 REST API，支持 GITHUB_TOKEN / GH_TOKEN 鉴权）
//  2. 下载 shr_<ver>_<os>_<arch>.tar.gz（windows 为 .zip）
//  3. 从归档中提取 shr 二进制
//  4. 原子替换当前可执行文件（Unix rename；Windows 先把旧文件移开）
package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	githubOwner = "havoc-rao"
	githubRepo  = "shell-rewrite"
	binaryName  = "shr"
)

// cmdUpdate 实现 shr update 自更新。
//
//	usage:
//	  shr update              更新到最新版本
//	  shr update --check      仅检查是否有新版本（不下载/不替换）
//	  shr update <version>    更新到指定版本（如 0.2.0 或 v0.2.0）
//	  shr update --check <version>
func cmdUpdate(args []string) int {
	checkOnly := false
	want := ""
	for _, a := range args {
		switch {
		case a == "--check" || a == "-check":
			checkOnly = true
		case a == "-":
			// ignore bare dash
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(os.Stderr, "shr update: unknown flag %q (see: shr help)\n", a)
			return 2
		default:
			if want != "" {
				fmt.Fprintln(os.Stderr, "shr update: too many arguments")
				return 2
			}
			want = a
		}
	}

	// 1. 解析目标版本 tag
	tag, err := resolveTag(want)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shr:", err)
		return 1
	}
	target := strings.TrimPrefix(tag, "v")

	label := "latest"
	if want != "" && want != "latest" {
		label = "target"
	}
	fmt.Printf("shr: current %s, %s %s\n", Version, label, target)

	// 2. 比较版本
	switch cmp := compareSemver(target, Version); {
	case cmp == 0:
		fmt.Println("shr: already up to date")
		return 0
	case cmp < 0:
		// 当前版本比最新 release 还新（多为 dev 构建），跳过降级。
		fmt.Printf("shr: current %s is newer than latest release %s; skipping\n", Version, target)
		return 0
	}
	if checkOnly {
		fmt.Println("shr: update available (run `shr update` to install)")
		return 0
	}

	// 3. 下载并替换
	if err := selfUpdate(tag); err != nil {
		fmt.Fprintln(os.Stderr, "shr:", err)
		return 1
	}
	fmt.Printf("shr: updated %s -> %s\n", Version, target)
	return 0
}

// resolveTag 把用户输入的 version 归一化为 git tag（vX.Y.Z）。
// 空或 "latest" 表示最新版。
func resolveTag(version string) (string, error) {
	if version == "" || version == "latest" {
		return latestTag()
	}
	if !strings.HasPrefix(version, "v") {
		version = "v" + version
	}
	return version, nil
}

// latestTag 解析最新 release 的 tag。
// 优先走 releases/latest 重定向（不受 api.github.com 60次/小时 限流影响），
// 失败再回退到 REST API（支持 GITHUB_TOKEN / GH_TOKEN 鉴权）。
func latestTag() (string, error) {
	if tag, ok := latestViaRedirect(); ok {
		return tag, nil
	}
	return latestViaAPI()
}

// latestViaRedirect 跟随 releases/latest 重定向，从最终 URL 提取 tag。
func latestViaRedirect() (string, bool) {
	url := fmt.Sprintf("https://github.com/%s/%s/releases/latest", githubOwner, githubRepo)
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	// 最终 URL 形如 .../releases/tag/v0.1.1
	p := resp.Request.URL.Path
	if i := strings.LastIndex(p, "/tag/"); i >= 0 {
		if tag := p[i+len("/tag/"):]; tag != "" {
			return tag, true
		}
	}
	return "", false
}

// latestViaAPI 走 REST API 解析最新 tag（可用 GITHUB_TOKEN 鉴权规避限流）。
func latestViaAPI() (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", githubOwner, githubRepo)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if tok := githubToken(); tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("could not resolve latest release (network issue or API rate limit): %w\nset GITHUB_TOKEN to bypass rate limit, or install via Go:\n  go install github.com/%s/%s/cmd/%s@latest",
			err, githubOwner, githubRepo, binaryName)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github API returned %s", resp.Status)
	}
	var body struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", err
	}
	if body.TagName == "" {
		return "", fmt.Errorf("could not parse latest release tag")
	}
	return body.TagName, nil
}

func githubToken() string {
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		return t
	}
	return os.Getenv("GH_TOKEN")
}

// selfUpdate 下载指定 tag 的归档、提取二进制并替换当前可执行文件。
func selfUpdate(tag string) error {
	osName, arch, ext, bin, err := platformInfo()
	if err != nil {
		return err
	}
	ver := strings.TrimPrefix(tag, "v")
	url := fmt.Sprintf("https://github.com/%s/%s/releases/download/%s/%s_%s_%s_%s.%s",
		githubOwner, githubRepo, tag, binaryName, ver, osName, arch, ext)

	fmt.Printf("shr: downloading %s\n", url)
	data, err := httpGetBytes(url)
	if err != nil {
		return fmt.Errorf("download failed for %s/%s: %w\nbrowse assets: https://github.com/%s/%s/releases/tag/%s\nor install via Go: go install github.com/%s/%s/cmd/%s@latest",
			osName, arch, err, githubOwner, githubRepo, tag, githubOwner, githubRepo, binaryName)
	}

	binData, err := extractBinary(data, ext, bin)
	if err != nil {
		return err
	}
	return replaceSelf(binData)
}

// platformInfo 返回当前平台的 release 归档参数。
func platformInfo() (osName, arch, ext, bin string, err error) {
	switch runtime.GOOS {
	case "darwin":
		osName = "darwin"
	case "linux":
		osName = "linux"
	case "windows":
		osName = "windows"
		bin = "shr.exe"
	default:
		return "", "", "", "", fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
	switch runtime.GOARCH {
	case "amd64":
		arch = "amd64"
	case "arm64":
		arch = "arm64"
	default:
		return "", "", "", "", fmt.Errorf("unsupported arch: %s", runtime.GOARCH)
	}
	if osName == "windows" {
		ext = "zip"
	} else {
		ext = "tar.gz"
	}
	if bin == "" {
		bin = binaryName
	}
	return
}

func httpGetBytes(url string) ([]byte, error) {
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// extractBinary 从归档中提取目标二进制。
func extractBinary(data []byte, ext, bin string) ([]byte, error) {
	switch ext {
	case "tar.gz":
		return extractFromTarGz(data, bin)
	case "zip":
		return extractFromZip(data, bin)
	default:
		return nil, fmt.Errorf("unknown archive format: %s", ext)
	}
}

func extractFromTarGz(data []byte, bin string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("invalid gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if filepath.Base(hdr.Name) == bin && hdr.Typeflag == tar.TypeReg {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("binary %q not found in archive", bin)
}

func extractFromZip(data []byte, bin string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("invalid zip: %w", err)
	}
	for _, f := range zr.File {
		if filepath.Base(f.Name) == bin && !f.FileInfo().IsDir() {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("binary %q not found in archive", bin)
}

// replaceSelf 原子替换当前可执行文件为新二进制内容。
// Unix：写入同目录临时文件后 rename 覆盖（原子）。
// Windows：运行中的 exe 不可覆盖，先把旧文件改名为 .old 再替换，最后删除。
func replaceSelf(binData []byte) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine executable path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	dir := filepath.Dir(exe)
	tmp, err := os.CreateTemp(dir, ".shr-update-*")
	if err != nil {
		return permsErr(err, dir)
	}
	tmpPath := tmp.Name()
	cleanup := func() { os.Remove(tmpPath) }

	if _, err := tmp.Write(binData); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		cleanup()
		return err
	}

	oldPath := ""
	if runtime.GOOS == "windows" {
		oldPath = exe + ".old"
		_ = os.Remove(oldPath)
		if err := os.Rename(exe, oldPath); err != nil {
			cleanup()
			return fmt.Errorf("cannot move current binary: %w", err)
		}
	}
	if err := os.Rename(tmpPath, exe); err != nil {
		if oldPath != "" {
			_ = os.Rename(oldPath, exe) // 尽力回滚
		}
		cleanup()
		return permsErr(err, exe)
	}
	if oldPath != "" {
		_ = os.Remove(oldPath)
	}
	return nil
}

// permsErr 把权限类错误转成带可操作建议的提示。
func permsErr(err error, path string) error {
	if os.IsPermission(err) {
		return fmt.Errorf("permission denied for %s: re-run with sudo, or use:\n  curl -fsSL https://raw.githubusercontent.com/%s/%s/main/scripts/install.sh | sudo sh",
			path, githubOwner, githubRepo)
	}
	return err
}

// compareSemver 比较 vX.Y.Z 形式版本号，返回 -1/0/1。
func compareSemver(a, b string) int {
	av := parseSemver(a)
	bv := parseSemver(b)
	for i := 0; i < 3; i++ {
		switch {
		case av[i] < bv[i]:
			return -1
		case av[i] > bv[i]:
			return 1
		}
	}
	return 0
}

// parseSemver 把 "v0.1.1" / "0.1.1-rc1" 解析为 [major,minor,patch]。
func parseSemver(s string) [3]int {
	s = strings.TrimPrefix(s, "v")
	var v [3]int
	parts := strings.SplitN(s, ".", 3)
	for i := 0; i < len(parts) && i < 3; i++ {
		parts[i] = strings.SplitN(parts[i], "-", 2)[0]
		n, _ := strconv.Atoi(parts[i])
		v[i] = n
	}
	return v
}
