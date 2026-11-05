#!/bin/bash

# Проверка наличия аргумента
if [ -z "$1" ]; then
    echo "Использование: ./create.sh <new_project_name>"
    exit 1
fi

NEW_PROJECT_NAME="$1"
CURRENT_DIR="$(pwd)"
BASE_DIR="$(basename "$CURRENT_DIR")"
NEW_DIR="${CURRENT_DIR}/../${NEW_PROJECT_NAME}"

# Проверка, что мы в папке lite
if [ "$BASE_DIR" != "lite" ]; then
    echo "Ошибка: Скрипт должен быть запущен из папки lite"
    exit 1
fi

# Проверка существования целевой папки
if [ -d "$NEW_DIR" ]; then
    echo "Ошибка: Папка $NEW_DIR уже существует"
    exit 1
fi

echo "Создание копии проекта..."
echo "Исходная директория: $CURRENT_DIR"
echo "Новая директория: $NEW_DIR"
echo "Новое имя проекта: $NEW_PROJECT_NAME"
echo ""

# Копирование проекта
cp -r "$CURRENT_DIR" "$NEW_DIR"

cd ${NEW_DIR}

# Сначала переименовываем папки
mv ./cmd/lite ./cmd/${NEW_PROJECT_NAME}
mv ./config/lite ./config/${NEW_PROJECT_NAME}

# Удаление лишних файлов и папок в internal/app
echo "Удаление лишних файлов в internal/app..."

# Удаляем лишние файлы из domain (оставляем только message.go)
rm -f ./internal/app/domain/callback.go
rm -f ./internal/app/domain/todolist.go
rm -f ./internal/app/domain/file.go
rm -f ./internal/app/domain/auth.go
rm -f ./internal/app/domain/init_data.go

# Удаляем папку repository
rm -rf ./internal/app/repository

# Удаляем папку services
rm -rf ./internal/app/services

# Удаляем папку docs/http
rm -rf ./docs/http

# Удаляем лишние файлы из subscriber
rm -f ./internal/app/subscriber/config.go

# Удаляем папку todolistctrl из router
rm -rf ./internal/app/router/todolistctrl
rm -rf ./internal/app/router/filectrl
rm -rf ./internal/app/router/authctrl

# Удаляем s3 клиента
rm -rf ./internal/app/clients/s3

# Удаляем тесты и testkit
rm -rf ./tests
rm -rf ./testkit

# Удаляем auth guideline
rm -f ./guidelines/auth.md

# Очищаем папку db/migrations, оставляем только .gitkeep
rm -rf ./db/migrations/*
# Создаём .gitkeep если его нет
touch ./db/migrations/.gitkeep

# Удаляем секцию "Пример приложения" из README.md
echo "Очистка README.md..."
sed -i '' '/### Пример приложения/,/^```$/d' ./README.md

# Замена "lite" на новое имя во всех текстовых файлах
# Используем sed с grep для фильтрации - только файлы, которые содержат "lite"
echo "Замена 'lite' на '${NEW_PROJECT_NAME}' в файлах..."
find . -type f \( -name "*.go" -o -name "*.yaml" -o -name "*.yml" -o -name "*.json" -o -name "*.md" -o -name "*.sh" -o -name "*.mod" -o -name "*.txt" -o -name "*.sql" -o -name ".gitignore" \) -not -path "./.git/*" -not -path "./vendor/*" | while read -r file; do
    if grep -q "lite" "$file" 2>/dev/null; then
        # Заменяем "lite" если перед ним НЕ буква/цифра и после него НЕ буква/цифра
        # Это позволит заменить "lite_redis", "my-lite-project", "postgres" останется как есть
        sed -i '' -E "s/(^|[^a-zA-Z0-9])lite([^a-zA-Z0-9]|$)/\1${NEW_PROJECT_NAME}\2/g" "$file"
    fi
done

# Исключение папок, которые не нужно копировать (git, vendor, deploy)
if [ -d "./.git" ]; then
    rm -rf "./.git"
    echo "Удалена папка .git"
fi

if [ -d "./vendor" ]; then
    rm -rf "${NEW_DIR}/vendor"
    echo "Удалена папка vendor"
fi

if [ -d "./deploy" ]; then
    rm -rf "${NEW_DIR}/deploy"
    echo "Удалена папка deploy"
fi


go mod tidy

make generate
# Инициализация нового git репозитория
git init
git add .
git commit -m "Initial commit"

echo ""
echo "Проект успешно создан в: $NEW_DIR"
echo "Имя проекта: $NEW_PROJECT_NAME"
echo "Все вхождения 'lite' заменены на '$NEW_PROJECT_NAME'"
