(function () {
    var preview = document.getElementById('preview');
    var statusDot = document.getElementById('statusDot');
    var statusText = document.getElementById('statusText');
    var ws;

    function connect() {
        var protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
        ws = new WebSocket(protocol + '//' + location.host + '/ws');

        ws.onopen = function () {
            statusDot.classList.remove('disconnected');
            statusText.textContent = 'Connected';
        };

        ws.onclose = function () {
            statusDot.classList.add('disconnected');
            statusText.textContent = 'Disconnected';
            setTimeout(connect, 2000);
        };

        ws.onmessage = function (event) {
            var data = event.data;

            // Server sends either rendered HTML or JSON messages.
            // JSON messages from the reader have a "type" field.
            try {
                var msg = JSON.parse(data);
                if (msg.type === 'error') {
                    console.error('Server error:', msg.error);
                    return;
                }
                // Ignore other JSON messages (content, save confirmations).
                return;
            } catch (e) {
                // Not JSON -- it's rendered HTML from the watcher.
            }

            preview.innerHTML = data;
            enhance();
        };
    }

    // Post-process: math rendering then syntax highlighting.
    // KaTeX runs first (innerHTML string ops), then highlight.js (DOM ops).
    function enhance() {
        // KaTeX: block math ($$...$$) -- must run before inline to avoid
        // partial matches.
        preview.innerHTML = preview.innerHTML.replace(
            /\$\$([\s\S]+?)\$\$/g,
            function (_, tex) {
                try {
                    return katex.renderToString(tex.trim(), { displayMode: true, throwOnError: false });
                } catch (e) {
                    return '$$' + tex + '$$';
                }
            }
        );

        // KaTeX: inline math ($...$), but not $$
        preview.innerHTML = preview.innerHTML.replace(
            /(?<!\$)\$(?!\$)(.+?)(?<!\$)\$(?!\$)/g,
            function (_, tex) {
                try {
                    return katex.renderToString(tex.trim(), { displayMode: false, throwOnError: false });
                } catch (e) {
                    return '$' + tex + '$';
                }
            }
        );

        // Syntax highlighting: the GFM library outputs bare <pre> without
        // <code> wrappers, so wrap content in <code> for highlight.js.
        preview.querySelectorAll('pre').forEach(function (pre) {
            if (!pre.querySelector('code')) {
                var code = document.createElement('code');
                code.textContent = pre.textContent;
                pre.textContent = '';
                pre.appendChild(code);
            }
            hljs.highlightElement(pre.querySelector('code'));
        });
    }

    connect();
})();
