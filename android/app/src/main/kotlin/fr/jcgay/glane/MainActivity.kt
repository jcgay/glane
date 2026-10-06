package fr.jcgay.glane

import android.app.Activity
import android.app.AlertDialog
import android.content.Intent
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.Environment
import android.provider.Settings
import android.view.WindowInsets
import android.webkit.RenderProcessGoneDetail
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Button
import android.widget.EditText
import android.widget.FrameLayout
import android.widget.LinearLayout
import android.widget.TextView
import android.window.OnBackInvokedDispatcher
import java.io.File
import java.net.InetSocketAddress
import java.net.ServerSocket
import java.net.Socket

private const val DEFAULT_DB = "/storage/emulated/0/Sync/glane/glane.db"

/**
 * Runs `glane serve --read-only` from the APK's native library dir and shows
 * it in a WebView. Everything glane does happens in that process; this class
 * only starts it, restarts it when Android killed it, and explains failures.
 */
class MainActivity : Activity() {
    private lateinit var root: FrameLayout
    private lateinit var web: WebView
    private var server: Process? = null
    private var ready: Process? = null // the server, once it answers on port
    private var resumed = false
    private var port = 0
    @Volatile private var lastLine = ""
    private val prefs by lazy { getSharedPreferences("glane", MODE_PRIVATE) }

    private var dbPath: String
        get() = prefs.getString("db", DEFAULT_DB)!!
        set(value) = prefs.edit().putString("db", value).apply()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        root = FrameLayout(this)
        // targetSdk 35+ draws edge to edge: keep content clear of the bars and the keyboard
        root.setOnApplyWindowInsetsListener { v, insets ->
            val bars = insets.getInsets(WindowInsets.Type.systemBars() or WindowInsets.Type.ime())
            v.setPadding(bars.left, bars.top, bars.right, bars.bottom)
            WindowInsets.CONSUMED
        }
        setContentView(root)
        web = WebView(this).apply {
            settings.javaScriptEnabled = true
            settings.domStorageEnabled = true
            webViewClient = object : WebViewClient() {
                override fun shouldOverrideUrlLoading(view: WebView, request: WebResourceRequest): Boolean {
                    if (request.url.host == "127.0.0.1") return false
                    // an item's link comes from imported data: no app may handle its scheme
                    runCatching { startActivity(Intent(Intent.ACTION_VIEW, request.url)) }
                    return true
                }

                // Android kills a background renderer to free memory; the default
                // (false) then kills the app too. Rebuild the activity instead.
                override fun onRenderProcessGone(view: WebView, detail: RenderProcessGoneDetail): Boolean {
                    recreate()
                    return true
                }
            }
        }
        if (Build.VERSION.SDK_INT >= 33) {
            onBackInvokedDispatcher.registerOnBackInvokedCallback(OnBackInvokedDispatcher.PRIORITY_DEFAULT) { back() }
        }
    }

    @Deprecated("Android 12 and older only; 13+ goes through onBackInvokedDispatcher")
    override fun onBackPressed() = back()

    private fun back() {
        if (root.getChildAt(0) === web && web.canGoBack()) web.goBack() else finish()
    }

    override fun onResume() {
        super.onResume()
        resumed = true
        start()
    }

    // web.url lives in memory: keep the page (and its ?q=) for when Android
    // kills the whole app or recreates the activity
    override fun onPause() {
        super.onPause()
        resumed = false
        web.url?.substringAfter("127.0.0.1:$port", "")?.takeIf { it.isNotEmpty() }
            ?.let { prefs.edit().putString("page", it).apply() }
    }

    override fun onDestroy() {
        server?.destroy()
        web.destroy()
        super.onDestroy()
    }

    private fun start() {
        // still running: show it, even if the file is missing for a moment, Go
        // keeps serving the last good copy. Dead (Android kills background
        // child processes): restart it below and come back to the same page.
        // Still starting: leave the "Starting" screen up until it answers.
        if (server?.isAlive == true) {
            if (ready === server) showWeb()
            return
        }
        if (!Environment.isExternalStorageManager()) {
            return show(
                "glane lit glane.db dans le dossier synchronisé par Syncthing : il a besoin de l'accès à tous les fichiers.",
                "Autoriser" to {
                    startActivity(Intent(Settings.ACTION_MANAGE_APP_ALL_FILES_ACCESS_PERMISSION, Uri.parse("package:$packageName")))
                },
            )
        }
        if (!File(dbPath).canRead()) {
            return show("glane.db introuvable à $dbPath", "Réglages" to ::askPath, "Réessayer" to ::start)
        }
        val resume = web.url?.substringAfter("127.0.0.1:$port", "/") ?: prefs.getString("page", "/")!!
        port = ServerSocket(0).use { it.localPort }
        lastLine = ""
        val p = ProcessBuilder(File(applicationInfo.nativeLibraryDir, "libglane.so").path, "serve", "--read-only", "--port", "$port")
            .redirectErrorStream(true)
            .apply { environment()["GLANE_DB"] = dbPath }
            .start()
        server = p
        val reader = Thread { p.inputStream.bufferedReader().forEachLine { lastLine = it } }.apply { start() }
        show("Démarrage de glane…")
        Thread {
            val deadline = System.currentTimeMillis() + 10_000
            while (p.isAlive && !listening(port) && System.currentTimeMillis() < deadline) Thread.sleep(50)
            if (p.isAlive && listening(port)) {
                runOnUiThread {
                    if (server !== p) return@runOnUiThread
                    ready = p
                    showWeb()
                    web.loadUrl("http://127.0.0.1:$port$resume")
                }
                // keep watching: a server dying under the page would leave
                // Chromium's "connection refused" in its place
                p.waitFor()
            } else {
                p.destroy()
            }
            // the error is glane's last line: let the reader get it first
            reader.join(1_000)
            runOnUiThread {
                // killed in the background: onResume restarts it
                if (server !== p || !resumed) return@runOnUiThread
                show("glane n'a pas démarré : $lastLine", "Relancer" to ::start, "Réglages" to ::askPath)
            }
        }.start()
    }

    private fun listening(port: Int) =
        runCatching { Socket().use { it.connect(InetSocketAddress("127.0.0.1", port), 200) } }.isSuccess

    private fun askPath() {
        val input = EditText(this).apply {
            setText(dbPath)
            setSingleLine()
        }
        AlertDialog.Builder(this)
            .setTitle("Chemin de glane.db")
            .setView(input)
            .setPositiveButton("OK") { _, _ ->
                dbPath = input.text.toString().trim()
                server?.destroy()
                server = null
                start()
            }
            .setNegativeButton("Annuler", null)
            .show()
    }

    private fun showWeb() {
        if (root.getChildAt(0) === web) return
        root.removeAllViews()
        root.addView(web)
    }

    private fun show(text: String, vararg actions: Pair<String, () -> Unit>) {
        val box = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(48, 48, 48, 48)
            addView(TextView(context).apply {
                this.text = text
                textSize = 16f
            })
            actions.forEach { (label, action) ->
                addView(Button(context).apply {
                    this.text = label
                    setOnClickListener { action() }
                })
            }
        }
        root.removeAllViews()
        root.addView(box)
    }
}
