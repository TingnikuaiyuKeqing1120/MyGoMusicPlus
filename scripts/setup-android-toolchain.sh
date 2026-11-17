#!/usr/bin/env bash
# One-time Android build toolchain into E:\Program\tools (JDK17 + SDK + Gradle).
set -x
T=/e/Program/tools
mkdir -p "$T"
cd "$T"

if [ ! -d jdk17 ]; then
  curl -sL -o jdk17.zip "https://api.adoptium.net/v3/binary/latest/17/ga/windows/x64/jdk/hotspot/normal/eclipse" || exit 1
  unzip -q jdk17.zip -d jdk17x || exit 1
  mv jdk17x/jdk-17* jdk17 || exit 1
fi
echo "JDK17_DONE"

if [ ! -d android-sdk/cmdline-tools/latest ]; then
  curl -sL -o cmdtools.zip "https://dl.google.com/android/repository/commandlinetools-win-11076708_latest.zip" || exit 1
  mkdir -p android-sdk/cmdline-tools
  unzip -q cmdtools.zip -d android-sdk/cmdline-tools || exit 1
  mv android-sdk/cmdline-tools/cmdline-tools android-sdk/cmdline-tools/latest || exit 1
fi
echo "CMDTOOLS_DONE"

if [ ! -d gradle-8.7 ]; then
  curl -sL -o gradle.zip "https://services.gradle.org/distributions/gradle-8.7-bin.zip" || exit 1
  unzip -q gradle.zip || exit 1
fi
echo "GRADLE_DONE"

export JAVA_HOME='E:\Program\tools\jdk17'
SDK='E:\Program\tools\android-sdk'
SM="$T/android-sdk/cmdline-tools/latest/bin/sdkmanager.bat"
yes | "$SM" --sdk_root="$SDK" --licenses >/dev/null 2>&1
"$SM" --sdk_root="$SDK" "platforms;android-35" "build-tools;35.0.0" "platform-tools" || exit 1
echo "SDK_DONE"
echo "TOOLCHAIN_READY"