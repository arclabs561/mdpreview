(function () {
    var url = 'ws://' + window.location.host + window.location.pathname + 'ws';
    var preview = document.getElementById("preview");
    var conn = new WebSocket(url);

    conn.onclose = function (event) {
        preview.textContent = 'connection closed';
    }
    conn.onmessage = function (event) {
        preview.innerHTML = event.data;
    }
})()