plugins {
    id("com.android.application")
    id("org.jetbrains.kotlin.android")
}

android {
    namespace = "com.mygomusic.plus"
    compileSdk = 36

    defaultConfig {
        applicationId = "com.mygomusic.plus"
        minSdk = 26
        targetSdk = 35
        // Rise with each release, together with the version in mygo.config.ts:
        // the system installer refuses a code below the installed one.
        versionCode = 10
        versionName = "0.5.2"
    }

    buildTypes {
        release {
            isMinifyEnabled = false
        }
    }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlinOptions {
        jvmTarget = "17"
    }
}

dependencies {
    implementation("androidx.core:core-ktx:1.13.1")
    implementation("androidx.media3:media3-exoplayer:1.11.1")
    implementation("androidx.media3:media3-datasource-okhttp:1.11.1")
    implementation("androidx.media3:media3-session:1.11.1")
    implementation("androidx.webkit:webkit:1.11.0")
}
