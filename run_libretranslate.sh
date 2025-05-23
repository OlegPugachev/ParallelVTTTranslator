#!/bin/bash

# Начальный порт
START_PORT=5000
PORT=$START_PORT

# Найти свободный порт
while lsof -i tcp:$PORT >/dev/null 2>&1; do
  ((PORT++))
done

echo "➡️  Свободный порт найден: $PORT"

# Запускаем контейнер

docker run -d \
  --name libretranslate_$PORT \
  -p $PORT:5000 \
  -e LT_LOAD_ONLY=en,ru \
  libretranslate/libretranslate

# Проверка запуска
if [ $? -eq 0 ]; then
  echo "✅ LibreTranslate запущен на http://localhost:$PORT"
else
  echo "❌ Ошибка запуска контейнера"
fi
