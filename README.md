# vtt-translator

`vtt-translator` is a CLI tool for batch-translating `.vtt` subtitle files 
from English into a specified language using a local
[LibreTranslate](https://github.com/LibreTranslate/LibreTranslate) API. 
It supports parallel processing, translation caching, and a global progress bar with ETA.

## 📦 Features

- 🔁 Recursively translates all `.vtt` files in a directory
- ⚡ Parallel processing with configurable worker count
- 📊 Global progress bar with ETA
- 🧠 Translation string caching to reduce API requests
- 🐞 Logs translation errors to `translate_errors.log`
- 🐳 Easy setup and launch of LibreTranslate via Docker (`run_libretranslate.sh`)

## 🚀 Quick Start

### 1. Install Dependencies

Ensure you have Go (1.18+) and Docker installed.

### 2. Launch LibreTranslate


### 2. Launch LibreTranslate

```bash
bash run_libretranslate.sh
```

LibreTranslate will launch on a free port. By default, the script loads English and Russian.

### 3. Build the Binary
   ```bash
   go build -o vtt-translator main.go
   ```
### 4. Run Translation
   bash
```
   ./vtt-translator --input path/to/folder --lang ru
```

### Parameters:

--input — path to a .vtt file or directory

--lang — target translation language (default: ru)

--workers — number of parallel workers (default: 5)

### 📂 Output
Each input file will be saved with a _<lang>.vtt suffix, e.g.:

example.vtt → example_ru.vtt

### ⚠️ Limitations
LibreTranslate must be available at http://localhost:5001/translate
Only .vtt files are supported
Only translation from English (en) is supported



