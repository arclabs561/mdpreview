# Welcome to mdpreview

A markdown editor/preview surface.

## Features Demo

### Text Formatting

You can use **bold**, *italic*, ~~strikethrough~~, and `inline code`.

### Math Equations

Inline math: $E = mc^2$

Block equations:

$$
\int_{-\infty}^{\infty} e^{-x^2} dx = \sqrt{\pi}
$$

$$
\frac{-b \pm \sqrt{b^2 - 4ac}}{2a}
$$

### Code Blocks

```javascript
function fibonacci(n) {
    if (n <= 1) return n;
    return fibonacci(n - 1) + fibonacci(n - 2);
}

console.log(fibonacci(10)); // 55
```

```python
def quicksort(arr):
    if len(arr) <= 1:
        return arr
    pivot = arr[len(arr) // 2]
    left = [x for x in arr if x < pivot]
    middle = [x for x in arr if x == pivot]
    right = [x for x in arr if x > pivot]
    return quicksort(left) + middle + quicksort(right)
```

### Tables

| Feature | Status | Notes |
|---------|--------|-------|
| WYSIWYG Editor | ✅ | Toast UI Editor |
| Math Support | ✅ | KaTeX rendering |
| Dark Mode | ✅ | Auto + manual |
| Syntax Highlighting | ✅ | Highlight.js |

### Task Lists

- [x] Add WYSIWYG editor
- [x] Implement dark mode
- [x] Add math rendering
- [ ] Add image upload
- [ ] Add collaborative editing
- [ ] Add export to PDF

### Links and Images

Repository: `https://github.com/arclabs561/mdpreview`.

### Quotes

> "The best way to predict the future is to invent it."
> — Alan Kay

### Lists

Unordered list:
- First item
- Second item
  - Nested item
  - Another nested item
- Third item

Ordered list:
1. First step
2. Second step
3. Third step

---

## Try It Out

1. Start editing this file in WYSIWYG mode (default)
2. Toggle to Markdown mode to see the raw markdown
3. Try the dark mode toggle
4. Save with `Ctrl+S` (auto-saves after 1 second)
5. Watch the status indicator show connection state
