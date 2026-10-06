plugins {
    id("com.android.application")
}

// A release passes its tag's version (-PglaneVersion=1.4.0); a local build is
// "dev". Android only installs an update whose versionCode is higher, so it is
// derived from the version: 1.4.0 -> 10400.
val glaneVersion = providers.gradleProperty("glaneVersion").getOrElse("dev")
val glaneVersionCode = Regex("""^(\d+)\.(\d+)\.(\d+)""").find(glaneVersion)
    ?.destructured?.let { (major, minor, patch) -> major.toInt() * 10000 + minor.toInt() * 100 + patch.toInt() }
    ?: 1

// The release key comes from the environment (CI secrets, see README). Without
// it a release build is left unsigned, and debug builds use the debug key.
fun env(name: String) = providers.environmentVariable(name).orNull

android {
    namespace = "fr.jcgay.glane"
    compileSdk = 36
    defaultConfig {
        applicationId = "fr.jcgay.glane"
        minSdk = 30
        targetSdk = 36
        versionCode = glaneVersionCode
        versionName = glaneVersion
        ndk { abiFilters += "arm64-v8a" }
    }
    env("GLANE_KEYSTORE")?.let { keystore ->
        signingConfigs.create("release") {
            storeFile = file(keystore)
            storePassword = env("GLANE_KEYSTORE_PASSWORD")
            keyAlias = env("GLANE_KEY_ALIAS")
            keyPassword = env("GLANE_KEY_PASSWORD")
        }
    }
    buildTypes {
        release { signingConfig = signingConfigs.findByName("release") }
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
    inputs.property("version", glaneVersion)
    outputs.file(out)
    workingDir(repoRoot)
    environment("GOOS", "android")
    environment("GOARCH", "arm64")
    environment("CGO_ENABLED", "0")
    commandLine("go", "build", "-trimpath", "-ldflags", "-s -w -X main.version=$glaneVersion", "-o", out.asFile.path, ".")
}
tasks.named("preBuild") { dependsOn(goBinary) }
