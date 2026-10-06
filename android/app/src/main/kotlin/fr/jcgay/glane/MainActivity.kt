package fr.jcgay.glane

import android.app.Activity
import android.app.AlertDialog
import android.content.Intent
import android.content.res.ColorStateList
import android.content.res.Configuration
import android.graphics.Color
import android.graphics.Typeface
import android.graphics.drawable.ColorDrawable
import android.graphics.drawable.GradientDrawable
import android.net.Uri
import android.os.Build
import android.os.Bundle
import android.os.Environment
import android.provider.Settings
import android.view.Gravity
import android.view.ViewGroup.LayoutParams.MATCH_PARENT
import android.view.ViewGroup.LayoutParams.WRAP_CONTENT
import android.view.WindowInsets
import android.view.WindowInsetsController
import android.webkit.RenderProcessGoneDetail
import android.webkit.WebResourceRequest
import android.webkit.WebView
import android.webkit.WebViewClient
import android.widget.Button
import android.widget.EditText
import android.widget.FrameLayout
import android.widget.LinearLayout
import android.widget.ProgressBar
import android.widget.ScrollView
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
    private var redraw: (() -> Unit)? = null
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
            setBackgroundColor(getColor(R.color.bg))
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
                getString(R.string.access_title),
                getString(R.string.access_body),
                null,
                getString(R.string.access_allow) to {
                    startActivity(Intent(Settings.ACTION_MANAGE_APP_ALL_FILES_ACCESS_PERMISSION, Uri.parse("package:$packageName")))
                },
            )
        }
        if (!File(dbPath).canRead()) {
            return show(
                getString(R.string.missing_title),
                getString(R.string.missing_body),
                dbPath,
                getString(R.string.change_path) to ::askPath, getString(R.string.retry) to ::start,
            )
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
        show(getString(R.string.starting))
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
                show(getString(R.string.failed_title), null, lastLine.ifBlank { null }, getString(R.string.restart) to ::start, getString(R.string.change_path) to ::askPath)
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
            .setTitle(R.string.path_title)
            .setView(input)
            .setPositiveButton(android.R.string.ok) { _, _ ->
                dbPath = input.text.toString().trim()
                server?.destroy()
                server = null
                start()
            }
            .setNegativeButton(android.R.string.cancel, null)
            .show()
    }

    private fun showWeb() {
        if (root.getChildAt(0) === web) return
        root.removeAllViews()
        root.addView(web)
    }

    private val Int.dp get() = (this * resources.displayMetrics.density).toInt()

    // the web UI's faces (assets/fonts, converted from internal/web/static),
    // so the native screens are lettered like the page that replaces them
    private fun geist(file: String, weight: Int) =
        Typeface.Builder(assets, "fonts/$file-Variable.ttf").setFontVariationSettings("'wght' $weight").build()
    private val sans by lazy { geist("Geist", 400) }
    private val sansMedium by lazy { geist("Geist", 560) }
    private val mono by lazy { geist("GeistMono", 400) }
    private val monoBold by lazy { geist("GeistMono", 600) }

    /**
     * A native screen in the web UI's colours: a title, an optional sentence,
     * an optional monospaced detail (a path, glane's error line), then the
     * actions, the first one filled. No action means work in progress.
     */
    private fun show(title: String, body: String? = null, code: String? = null, vararg actions: Pair<String, () -> Unit>) {
        redraw = { show(title, body, code, *actions) }
        fun rounded(fill: Int, stroke: Int? = null) = GradientDrawable().apply {
            cornerRadius = 8.dp.toFloat()
            setColor(fill)
            stroke?.let { setStroke(1.dp, it) }
        }
        val box = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            gravity = Gravity.CENTER_VERTICAL
            setPadding(28.dp, 24.dp, 28.dp, 32.dp)
            addView(TextView(context).apply {
                text = "glane"
                typeface = monoBold
                textSize = 17f
                setTextColor(getColor(R.color.ink))
                val mark = rounded(getColor(R.color.accent)).apply { cornerRadius = 2.dp.toFloat(); setBounds(0, 0, 9.dp, 9.dp) }
                setCompoundDrawablesRelative(mark, null, null, null)
                compoundDrawablePadding = 10.dp
            }, LinearLayout.LayoutParams(WRAP_CONTENT, WRAP_CONTENT).apply { bottomMargin = 40.dp })
            addView(TextView(context).apply {
                text = title
                textSize = 24f
                typeface = sansMedium
                letterSpacing = -0.01f
                setTextColor(getColor(R.color.ink))
            })
            body?.let {
                addView(TextView(context).apply {
                    text = it
                    textSize = 16f
                    typeface = sans
                    setLineSpacing(0f, 1.25f)
                    setTextColor(getColor(R.color.muted))
                }, LinearLayout.LayoutParams(MATCH_PARENT, WRAP_CONTENT).apply { topMargin = 10.dp })
            }
            code?.let {
                addView(TextView(context).apply {
                    text = it
                    typeface = mono
                    textSize = 13f
                    setTextIsSelectable(true)
                    setTextColor(getColor(R.color.muted))
                    background = rounded(getColor(R.color.surface), getColor(R.color.border_strong))
                    setPadding(14.dp, 12.dp, 14.dp, 12.dp)
                }, LinearLayout.LayoutParams(MATCH_PARENT, WRAP_CONTENT).apply { topMargin = 16.dp })
            }
            if (actions.isEmpty()) {
                addView(ProgressBar(context, null, android.R.attr.progressBarStyleHorizontal).apply {
                    isIndeterminate = true
                    indeterminateTintList = ColorStateList.valueOf(getColor(R.color.accent))
                }, LinearLayout.LayoutParams(MATCH_PARENT, WRAP_CONTENT).apply { topMargin = 20.dp })
            }
            actions.forEachIndexed { i, (label, action) ->
                addView(Button(context).apply {
                    text = label
                    isAllCaps = false
                    textSize = 15f
                    typeface = sansMedium
                    stateListAnimator = null
                    if (i == 0) {
                        setTextColor(getColor(R.color.on_accent))
                        background = rounded(getColor(R.color.accent))
                    } else {
                        setTextColor(getColor(R.color.ink))
                        background = rounded(Color.TRANSPARENT, getColor(R.color.border_strong))
                    }
                    setOnClickListener { action() }
                }, LinearLayout.LayoutParams(MATCH_PARENT, 52.dp).apply { topMargin = if (i == 0) 28.dp else 10.dp })
            }
        }
        root.removeAllViews()
        root.addView(ScrollView(this).apply {
            isFillViewport = true
            addView(box)
        })
    }

    // configChanges keeps the activity on a dark/light switch: repaint what
    // was drawn with the old colours
    override fun onConfigurationChanged(newConfig: Configuration) {
        super.onConfigurationChanged(newConfig)
        window.setBackgroundDrawable(ColorDrawable(getColor(R.color.bg)))
        web.setBackgroundColor(getColor(R.color.bg))
        val bars = WindowInsetsController.APPEARANCE_LIGHT_STATUS_BARS or WindowInsetsController.APPEARANCE_LIGHT_NAVIGATION_BARS
        window.insetsController?.setSystemBarsAppearance(if (resources.getBoolean(R.bool.light_bars)) bars else 0, bars)
        if (root.getChildAt(0) !== web) redraw?.invoke()
    }
}
