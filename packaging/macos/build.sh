#!/bin/bash
set -euo pipefail

cd "$(dirname "$0")/../.."
ROOT=$(pwd)
OUT="$ROOT/dist/macos"
VERSION=$(sed -n 's/^var version = "\(v[^"]*\)".*/\1/p' cmd/F_ckCensor/main.go)
VERSION=${VERSION:-dev}
LDVER=""
if [ -n "${FKC_VERSION:-}" ]; then
	VERSION=$FKC_VERSION
	LDVER="-X main.version=$FKC_VERSION"
fi
APP="$OUT/F_ckCensor.app"
echo "F_ckCensor $VERSION → $OUT"

command -v go >/dev/null || { echo "нет Go: установите с https://go.dev/dl"; exit 1; }
command -v clang >/dev/null || { echo "нет инструментов Xcode: xcode-select --install"; exit 1; }

rm -rf "$OUT"
mkdir -p "$OUT/bin"

for ARCH in arm64 amd64; do
	echo "сборка ${ARCH}…"
	CGO_ENABLED=1 GOOS=darwin GOARCH=$ARCH MACOSX_DEPLOYMENT_TARGET=11.0 \
		go build -trimpath -ldflags "-s -w $LDVER" -o "$OUT/bin/F_ckCensor-$ARCH" ./cmd/F_ckCensor
done
lipo -create -output "$OUT/bin/F_ckCensor" "$OUT/bin/F_ckCensor-arm64" "$OUT/bin/F_ckCensor-amd64"
lipo -info "$OUT/bin/F_ckCensor"

# 2. Приложение
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp "$OUT/bin/F_ckCensor" "$APP/Contents/MacOS/F_ckCensor"

# иконка: из assets/icon_black.ico — набор размеров и .icns
ICONSET="$OUT/icon.iconset"
mkdir -p "$ICONSET"
sips -s format png assets/icon_black.ico --out "$OUT/icon.png" >/dev/null
for S in 16 32 128 256 512; do
	sips -z $S $S "$OUT/icon.png" --out "$ICONSET/icon_${S}x${S}.png" >/dev/null
	D=$((S * 2))
	sips -z $D $D "$OUT/icon.png" --out "$ICONSET/icon_${S}x${S}@2x.png" >/dev/null
done
iconutil -c icns "$ICONSET" -o "$APP/Contents/Resources/icon.icns"

SHORT=${VERSION#v}
cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key>
	<string>F_ckCensor</string>
	<key>CFBundleDisplayName</key>
	<string>F_ckCensor</string>
	<key>CFBundleIdentifier</key>
	<string>ru.s4yn3x.fckcensor</string>
	<key>CFBundleExecutable</key>
	<string>F_ckCensor</string>
	<key>CFBundleIconFile</key>
	<string>icon</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleShortVersionString</key>
	<string>$SHORT</string>
	<key>CFBundleVersion</key>
	<string>$SHORT</string>
	<key>LSMinimumSystemVersion</key>
	<string>11.0</string>
	<key>LSUIElement</key>
	<true/>
	<key>NSHighResolutionCapable</key>
	<true/>
	<key>NSAppleEventsUsageDescription</key>
	<string>Чтобы закрывать и перезапускать Яндекс Музыку при установке патча.</string>
</dict>
</plist>
PLIST

codesign --force --deep --sign - --timestamp=none "$APP"
codesign --verify --deep --strict "$APP"

ZIP="$OUT/F_ckCensor-mac.zip"
ditto -c -k --keepParent "$APP" "$ZIP"

STAGE="$OUT/dmg"
mkdir -p "$STAGE"
cp -R "$APP" "$STAGE/"
ln -s /Applications "$STAGE/Applications"
DMG="$OUT/F_ckCensor-$VERSION-universal.dmg"
hdiutil create -volname "F_ckCensor" -srcfolder "$STAGE" -ov -format UDZO "$DMG" >/dev/null
rm -rf "$STAGE" "$ICONSET" "$OUT/icon.png" "$OUT/bin/F_ckCensor-arm64" "$OUT/bin/F_ckCensor-amd64"

echo
echo "готово:"
echo "  приложение: $APP"
echo "  установщик: $DMG"
echo "  для автообновления: $ZIP"
