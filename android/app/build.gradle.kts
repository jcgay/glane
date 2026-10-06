plugins {
    id("com.android.application")
}

android {
    namespace = "fr.jcgay.glane"
    compileSdk = 36
    defaultConfig {
        applicationId = "fr.jcgay.glane"
        minSdk = 30
        targetSdk = 36
        versionCode = 1
        versionName = "0.1"
        ndk { abiFilters += "arm64-v8a" }
    }
    // the Go server is launched as a process: it must sit on disk, in the
    // app's native library dir, the one place Android lets an app execute
    packaging { jniLibs { useLegacyPackaging = true } }
}

val repoRoot = rootProject.layout.projectDirectory.dir("..")
val goBinary = tasks.register<Exec>("goBinary") {
    description = "Cross-compiles glane for Android as libglane.so"
    val out = layout.projectDirectory.file("src/main/jniLibs/arm64-v8a/libglane.so")
    inputs.files(fileTree(repoRoot) {
        include("**/*.go", "go.mod", "go.sum", "internal/web/templates/**", "internal/web/static/**")
        exclude("android/**")
    })
    outputs.file(out)
    workingDir(repoRoot)
    environment("GOOS", "android")
    environment("GOARCH", "arm64")
    environment("CGO_ENABLED", "0")
    commandLine("go", "build", "-trimpath", "-ldflags", "-s -w", "-o", out.asFile.path, ".")
}
tasks.named("preBuild") { dependsOn(goBinary) }
