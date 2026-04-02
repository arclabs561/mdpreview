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
            preview.innerHTML = event.data;
            renderMath();
        };
    }

    // Post-process: KaTeX math rendering.
    function renderMath() {
        // Block math ($$...$$)
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

        // Inline math ($...$), but not $$
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
    }

    connect();
})();
