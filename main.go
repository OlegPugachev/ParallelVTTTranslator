package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/schollz/progressbar/v3"
	"golang.org/x/sync/semaphore"
)

const translateURL = "http://localhost:5001/translate"

type TranslateRequest struct {
	Q      string `json:"q"`
	Source string `json:"source"`
	Target string `json:"target"`
	Format string `json:"format"`
}

type TranslateResponse struct {
	TranslatedText string `json:"translatedText"`
}

var (
	errorLog         *os.File
	translationCache sync.Map
	fileCounter      int64
	lineCounter      int64
	globalBar        *progressbar.ProgressBar
)

var (
	inputPath  string
	targetLang string
	workers    int
)

func init() {
	flag.StringVar(&inputPath, "input", "", "Path to a file or directory")
	flag.StringVar(&targetLang, "lang", "ru", "Target translation language")
	flag.IntVar(&workers, "workers", 5, "Number of parallel workers")
	flag.Parse()
}

func main() {
	if inputPath == "" {
		fmt.Println("Please specify path with --input and language with --lang")
		os.Exit(1)
	}

	var err error
	errorLog, err = os.OpenFile("translate_errors.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		fmt.Printf("Failed to open error log file: %v\n", err)
		os.Exit(1)
	}
	defer func() {
		if err := errorLog.Close(); err != nil {
			fmt.Printf("⚠️ Failed to close error log: %v", err)
		}
	}()

	info, err := os.Stat(inputPath)
	if err != nil {
		logError(fmt.Sprintf("Access error: %v", err))
		os.Exit(1)
	}

	start := time.Now()

	if info.IsDir() {
		// Pre-count total lines for global progress bar
		totalLines := countTotalLines(inputPath)
		globalBar = progressbar.NewOptions(totalLines,
			progressbar.OptionSetDescription("Total Progress"),
			progressbar.OptionShowCount(),
			progressbar.OptionShowIts(),
			progressbar.OptionSetPredictTime(true),
			progressbar.OptionFullWidth())
		err = processDirectory(inputPath, targetLang)
	} else {
		globalBar = progressbar.NewOptions(1,
			progressbar.OptionSetDescription("Progress"),
			progressbar.OptionShowCount(),
			progressbar.OptionShowIts(),
			progressbar.OptionSetPredictTime(true),
			progressbar.OptionFullWidth())
		err = processFile(inputPath, targetLang)
	}

	duration := time.Since(start)
	fmt.Printf("\n✅ Completed: %d files, %d lines in %v\n", fileCounter, lineCounter, duration)
	if err != nil {
		logError(fmt.Sprintf("Processing error: %v", err))
		os.Exit(1)
	}
}

func isSubtitleFile(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".vtt") || strings.HasSuffix(lower, ".srt")
}

func countTotalLines(root string) int {
	var total int64
	errWalk := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && !strings.HasPrefix(info.Name(), ".") && isSubtitleFile(info.Name()) {
			f, err := os.Open(path)
			if err == nil {
				scanner := bufio.NewScanner(f)
				for scanner.Scan() {
					total++
				}
				if closeErr := f.Close(); closeErr != nil {
					logError(fmt.Sprintf("Failed to close file %s: %v", path, closeErr))
				}
			} else {
				logError(fmt.Sprintf("Failed to open file %s: %v", path, err))
			}
		}
		return nil
	})
	if errWalk != nil {
		logError(fmt.Sprintf("Line counting error: %v", errWalk))
	}
	return int(total)
}

func processDirectory(dirPath, lang string) error {
	var wg sync.WaitGroup
	sem := semaphore.NewWeighted(int64(workers))

	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			logError(fmt.Sprintf("Walk error %s: %v", path, err))
			return nil
		}

		if !info.IsDir() && !strings.HasPrefix(info.Name(), ".") && isSubtitleFile(info.Name()) {
			wg.Add(1)
			if err := sem.Acquire(context.Background(), 1); err != nil {
				logError(fmt.Sprintf("Semaphore error: %v", err))
				wg.Done()
				return nil
			}

			go func(p string) {
				defer wg.Done()
				defer sem.Release(1)
				defer func() {
					if r := recover(); r != nil {
						logError(fmt.Sprintf("Panic in file %s: %v", p, r))
					}
				}()

				if err := processFile(p, lang); err != nil {
					logError(fmt.Sprintf("Translation error %s: %v", p, err))
				}
			}(path)
		}
		return nil
	})

	wg.Wait()
	return err
}

func processFile(inputPath, lang string) error {
	file, err := os.Open(inputPath)
	if err != nil {
		return err
	}
	defer func(file *os.File) {
		err := file.Close()
		if err != nil {
			logError(fmt.Sprintf("Failed to close file %s: %v", inputPath, err))
		}
	}(file)

	scanner := bufio.NewScanner(file)
	type indexedLine struct {
		index int
		text  string
	}

	var lines []indexedLine
	index := 0

	for scanner.Scan() {
		text := scanner.Text()
		lines = append(lines, indexedLine{index: index, text: text})
		index++
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	results := make([]string, len(lines))
	var wg sync.WaitGroup
	sem := semaphore.NewWeighted(int64(workers))

	for _, line := range lines {
		wg.Add(1)
		if err := sem.Acquire(context.Background(), 1); err != nil {
			logError(fmt.Sprintf("Line semaphore error: %v", err))
			wg.Done()
			continue
		}

		go func(l indexedLine) {
			defer wg.Done()
			defer sem.Release(1)

			// Skipping subtitle service lines
			if strings.Contains(l.text, "-->") || strings.TrimSpace(l.text) == "" || l.text == "WEBVTT" {
				results[l.index] = l.text
				_ = globalBar.Add(1)
				return
			}

			translated, err := translateText(l.text, lang)
			if err != nil {
				logError(fmt.Sprintf("Line error in file '%s' [line %d]: '%s' — %v", inputPath, l.index+1, l.text, err))
				results[l.index] = l.text // Сохраняем оригинал при ошибке
			} else {
				results[l.index] = translated
				atomic.AddInt64(&lineCounter, 1)
			}
			_ = globalBar.Add(1)
		}(line)
	}

	wg.Wait()
	output := strings.Join(results, "\n")
	outputPath := getOutputPath(inputPath, lang)
	atomic.AddInt64(&fileCounter, 1)
	return os.WriteFile(outputPath, []byte(output), 0644)
}

func translateText(text, lang string) (string, error) {
	text = strings.TrimSpace(text)
	if val, ok := translationCache.Load(text); ok {
		return val.(string), nil
	}

	req := TranslateRequest{
		Q:      text,
		Source: "en",
		Target: lang,
		Format: "text",
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	reqHTTP, err := http.NewRequestWithContext(ctx, "POST", translateURL, bytes.NewBuffer(body))
	if err != nil {
		return "", err
	}
	reqHTTP.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(reqHTTP)
	if err != nil {
		return "", err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			logError(fmt.Sprintf("Failed to close response body: %v", err))
		}
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("API response: %s", resp.Status)
	}

	var res TranslateResponse
	err = json.NewDecoder(resp.Body).Decode(&res)
	if err != nil {
		return "", err
	}

	translationCache.Store(text, res.TranslatedText)
	return res.TranslatedText, nil
}

func getOutputPath(inputPath, lang string) string {
	ext := filepath.Ext(inputPath)
	base := strings.TrimSuffix(inputPath, ext)
	return base + "_" + lang + ext
}

func logError(message string) {
	fmt.Println("⚠️", message)
	_, err := errorLog.WriteString(message + "\n")
	if err != nil {
		return
	}
}
