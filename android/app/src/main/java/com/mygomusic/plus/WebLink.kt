package com.mygomusic.plus

// WebLink delivers asynchronous Android results to the web application.
object WebLink {
    // device answers the app's GitHub sign-in requests, by the id each went
    // out with; install reports progress and the version of a saved APK.
    @Volatile
    var device: ((Int, String) -> Unit)? = null

    @Volatile
    var install: ((String) -> Unit)? = null

}
