(function () {
    let editor;
    let ws;
    let isConnected = false;
    let isDirty = false;
    let saveTimeout;
    let currentMode = 'wysiwyg';
    let isDarkMode = window.matchMedia('(prefers-color-scheme: dark)').matches;

    // Initialize Toast UI Editor
    function initEditor() {
        editor = new toastui.Editor({
            el: document.querySelector('#editor'),
            height: '100%',
            initialEditType: 'wysiwyg',
            previewStyle: 'vertical',
            usageStatistics: false,
            toolbarItems: [
                ['heading', 'bold', 'italic', 'strike'],
                ['hr', 'quote'],
                ['ul', 'ol', 'task', 'indent', 'outdent'],
                ['table', 'link', 'image'],
                ['code', 'codeblock'],
                ['scrollSync'],
            ],
            hooks: {
                addImageBlobHook: function (blob, callback) {
                    // Handle image upload - for now just show data URL
                    const reader = new FileReader();
                    reader.onload = function(e) {
                        callback(e.target.result, 'Image');
                    };
                    reader.readAsDataURL(blob);
                }
            },
            customHTMLRenderer: {
                // Add KaTeX rendering support
                htmlBlock: {
                    math(node) {
                        const latex = node.literal;
                        try {
                            return [
                                { type: 'openTag', tagName: 'div', outerNewLine: true, attributes: { class: 'math-block' } },
                                { type: 'html', content: katex.renderToString(latex, { throwOnError: false, displayMode: true }) },
                                { type: 'closeTag', tagName: 'div', outerNewLine: true }
                            ];
                        } catch (e) {
                            return [
                                { type: 'text', content: latex }
                            ];
                        }
                    }
                },
                htmlInline: {
                    math(node) {
                        const latex = node.literal;
                        try {
                            return [
                                { type: 'html', content: katex.renderToString(latex, { throwOnError: false, displayMode: false }) }
                            ];
                        } catch (e) {
                            return [
                                { type: 'text', content: latex }
                            ];
                        }
                    }
                }
            }
        });

        // Listen for changes
        editor.on('change', () => {
            isDirty = true;
            showSaveIndicator('Unsaved changes...', false);
            
            // Auto-save after 1 second of inactivity
            clearTimeout(saveTimeout);
            saveTimeout = setTimeout(() => {
                saveFile();
            }, 1000);
        });

        // Initialize WebSocket connection
        initWebSocket();
    }

    function initWebSocket() {
        const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
        const wsUrl = protocol + '//' + window.location.host + '/ws';
        
        ws = new WebSocket(wsUrl);

        ws.onopen = function () {
            isConnected = true;
            updateStatus(true);
            console.log('WebSocket connected');
        };

        ws.onclose = function () {
            isConnected = false;
            updateStatus(false);
            console.log('WebSocket disconnected');
            
            // Attempt to reconnect after 3 seconds
            setTimeout(() => {
                console.log('Attempting to reconnect...');
                initWebSocket();
            }, 3000);
        };

        ws.onerror = function (error) {
            console.error('WebSocket error:', error);
            updateStatus(false);
        };

        ws.onmessage = function (event) {
            const message = event.data;
            
            try {
                // Try to parse as JSON first
                const data = JSON.parse(message);
                
                if (data.type === 'content') {
                    // Initial content load
                    if (editor && !isDirty) {
                        editor.setMarkdown(data.content);
                    }
                } else if (data.type === 'error') {
                    console.error('Server error:', data.error);
                    showSaveIndicator('Error: ' + data.error, false);
                }
            } catch (e) {
                // Not JSON, ignore (we're in WYSIWYG mode, don't need HTML preview)
                console.debug('Received non-JSON message (likely preview HTML)');
            }
        };
    }

    function updateStatus(connected) {
        const dot = document.getElementById('statusDot');
        const text = document.getElementById('statusText');
        
        if (connected) {
            dot.classList.remove('disconnected');
            text.textContent = 'Connected';
        } else {
            dot.classList.add('disconnected');
            text.textContent = 'Disconnected';
        }
    }

    function showSaveIndicator(message, isSuccess = true) {
        const indicator = document.getElementById('saveIndicator');
        const icon = indicator.querySelector('span:first-child');
        const text = indicator.querySelector('span:last-child');
        
        icon.textContent = isSuccess ? '✓' : '⚠';
        text.textContent = message;
        indicator.classList.add('visible');
    }

    function hideSaveIndicator() {
        const indicator = document.getElementById('saveIndicator');
        setTimeout(() => {
            indicator.classList.remove('visible');
        }, 2000);
    }

    window.saveFile = function() {
        if (!editor || !isConnected) {
            showSaveIndicator('Not connected', false);
            return;
        }
        
        const content = editor.getMarkdown();
        
        // Send save request via WebSocket
        ws.send(JSON.stringify({
            type: 'save',
            content: content
        }));
        
        isDirty = false;
        showSaveIndicator('Saved', true);
        hideSaveIndicator();
    };

    window.switchMode = function(mode) {
        if (!editor) return;
        
        currentMode = mode;
        
        // Update UI
        document.querySelectorAll('.mode-toggle button').forEach(btn => {
            if (btn.dataset.mode === mode) {
                btn.classList.add('active');
            } else {
                btn.classList.remove('active');
            }
        });
        
        // Switch editor mode
        if (mode === 'wysiwyg') {
            editor.changeMode('wysiwyg');
        } else {
            editor.changeMode('markdown');
        }
    };

    window.toggleTheme = function() {
        isDarkMode = !isDarkMode;
        const icon = document.getElementById('themeIcon');
        
        if (isDarkMode) {
            document.documentElement.style.setProperty('--bg-primary', '#0d1117');
            document.documentElement.style.setProperty('--bg-secondary', '#161b22');
            document.documentElement.style.setProperty('--border-color', '#30363d');
            document.documentElement.style.setProperty('--text-primary', '#e6edf3');
            document.documentElement.style.setProperty('--text-secondary', '#8b949e');
            document.documentElement.style.setProperty('--editor-bg', '#0d1117');
            icon.textContent = '☀️';
        } else {
            document.documentElement.style.setProperty('--bg-primary', '#ffffff');
            document.documentElement.style.setProperty('--bg-secondary', '#f6f8fa');
            document.documentElement.style.setProperty('--border-color', '#d0d7de');
            document.documentElement.style.setProperty('--text-primary', '#24292f');
            document.documentElement.style.setProperty('--text-secondary', '#57606a');
            document.documentElement.style.setProperty('--editor-bg', '#ffffff');
            icon.textContent = '🌙';
        }
    };

    // Keyboard shortcuts
    document.addEventListener('keydown', (e) => {
        // Ctrl/Cmd + S to save
        if ((e.ctrlKey || e.metaKey) && e.key === 's') {
            e.preventDefault();
            saveFile();
        }
        
        // Ctrl/Cmd + Shift + M to toggle mode
        if ((e.ctrlKey || e.metaKey) && e.shiftKey && e.key === 'M') {
            e.preventDefault();
            switchMode(currentMode === 'wysiwyg' ? 'markdown' : 'wysiwyg');
        }
    });

    // Warn before leaving if there are unsaved changes
    window.addEventListener('beforeunload', (e) => {
        if (isDirty) {
            e.preventDefault();
            e.returnValue = '';
        }
    });

    // Initialize when DOM is ready
    if (document.readyState === 'loading') {
        document.addEventListener('DOMContentLoaded', initEditor);
    } else {
        initEditor();
    }
})();