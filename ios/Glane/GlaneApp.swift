import SwiftUI
import WebKit

@main
struct GlaneApp: App {
    var body: some Scene {
        WindowGroup { ContentView() }
    }
}

// iOS only lets the app back into a folder the user picked: keep it as a bookmark
private let folderKey = "folder"

/// Asks once for the folder a sync app (Möbius Sync, iCloud Drive…) keeps
/// glane.db in, then shows the web UI over it.
struct ContentView: View {
    @State private var ready = false
    @State private var picking = false
    @State private var error: String?

    var body: some View {
        Group {
            if ready {
                WebView()
            } else {
                VStack(alignment: .leading, spacing: 16) {
                    Text(verbatim: "glane").font(.system(.headline, design: .monospaced))
                    Text("Choose the glane folder").font(.title.weight(.medium))
                    Text("Pick the folder your sync app keeps glane.db in. glane only reads it.")
                        .foregroundStyle(.secondary)
                    if let error {
                        Text(verbatim: error).font(.system(.footnote, design: .monospaced)).textSelection(.enabled)
                    }
                    Button("Choose folder") { picking = true }.buttonStyle(.borderedProminent)
                }
                .padding(28)
            }
        }
        .fileImporter(isPresented: $picking, allowedContentTypes: [.folder]) { result in
            guard case .success(let url) = result else { return }
            let scoped = url.startAccessingSecurityScopedResource()
            defer { if scoped { url.stopAccessingSecurityScopedResource() } }
            UserDefaults.standard.set(try? url.bookmarkData(), forKey: folderKey)
            open()
        }
        .onAppear(perform: open)
    }

    private func open() {
        guard let bookmark = UserDefaults.standard.data(forKey: folderKey) else { return }
        var stale = false
        guard let folder = try? URL(resolvingBookmarkData: bookmark, bookmarkDataIsStale: &stale) else {
            error = String(localized: "The folder is gone. Choose it again.")
            return
        }
        if stale { UserDefaults.standard.set(try? folder.bookmarkData(), forKey: folderKey) }
        // never stopped: Go reopens the file whenever the sync app replaces it
        _ = folder.startAccessingSecurityScopedResource()
        if let msg = GlaneOpen(folder.appendingPathComponent("glane.db").path) {
            error = String(cString: msg)
            free(msg)
            return
        }
        ready = true
    }
}

struct WebView: UIViewRepresentable {
    func makeCoordinator() -> Coordinator { Coordinator() }

    func makeUIView(context: Context) -> WKWebView {
        let config = WKWebViewConfiguration()
        config.setURLSchemeHandler(context.coordinator, forURLScheme: "glane")
        let web = WKWebView(frame: .zero, configuration: config)
        web.navigationDelegate = context.coordinator
        web.allowsBackForwardNavigationGestures = true
        web.load(URLRequest(url: URL(string: "glane://app/")!))
        return web
    }

    func updateUIView(_: WKWebView, context: Context) {}
}

private struct BridgeRequest: Encodable {
    let method: String
    let url: String
    let header: [String: String]
    let body: Data?
}

private struct BridgeResponse: Decodable {
    let status: Int
    let header: [String: String]?
    let body: Data?
}

/// Serves glane:// from the Go handler linked into the app (ios/bridge).
final class Coordinator: NSObject, WKURLSchemeHandler, WKNavigationDelegate {
    private var stopped = Set<ObjectIdentifier>() // main thread only

    func webView(_: WKWebView, start task: WKURLSchemeTask) {
        let url = task.request.url!
        let parts = URLComponents(url: url, resolvingAgainstBaseURL: false)
        var target = parts?.percentEncodedPath ?? ""
        if target.isEmpty { target = "/" }
        if let query = parts?.percentEncodedQuery { target += "?" + query }
        var header = task.request.allHTTPHeaderFields ?? [:]
        // the UI speaks French or English from it; WebKit may leave it out on a custom scheme
        if header["Accept-Language"] == nil {
            header["Accept-Language"] = Locale.preferredLanguages.joined(separator: ", ")
        }
        let input = try! JSONEncoder().encode(BridgeRequest(
            method: task.request.httpMethod ?? "GET", url: target, header: header, body: task.request.httpBody))
        let id = ObjectIdentifier(task)
        // a search takes milliseconds, but keep the main thread free anyway
        DispatchQueue.global(qos: .userInitiated).async {
            let out = GlaneRequest(String(decoding: input, as: UTF8.self))!
            let res = try? JSONDecoder().decode(BridgeResponse.self, from: Data(String(cString: out).utf8))
            free(out)
            DispatchQueue.main.async {
                // WebKit raises if a stopped task gets a response
                if self.stopped.remove(id) != nil { return }
                let http = HTTPURLResponse(
                    url: url, statusCode: res?.status ?? 500, httpVersion: "HTTP/1.1", headerFields: res?.header)!
                task.didReceive(http)
                task.didReceive(res?.body ?? Data())
                task.didFinish()
            }
        }
    }

    func webView(_: WKWebView, stop task: WKURLSchemeTask) {
        stopped.insert(ObjectIdentifier(task))
    }

    // an item's link comes from imported data: only web links leave the app, to Safari
    func webView(
        _: WKWebView, decidePolicyFor action: WKNavigationAction,
        decisionHandler: @escaping (WKNavigationActionPolicy) -> Void
    ) {
        guard let url = action.request.url, url.scheme != "glane" else { return decisionHandler(.allow) }
        if url.scheme == "http" || url.scheme == "https" { UIApplication.shared.open(url) }
        decisionHandler(.cancel)
    }
}
