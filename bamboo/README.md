# Bamboo CSS

A classless CSS library which adds nice default style for all HTML elements. It saves you a lot of time when you need to style HTML for your HTML/React/Vue demo on CodePen/CodeSandbox. It can also be used as a base style for your blog/website.

Bamboo CSS uses [modern-normalize](https://github.com/sindresorhus/modern-normalize) and [sanitize.css](https://github.com/csstools/sanitize.css) to ensure consistent styling across browsers (no IE support). When using Bamboo CSS, you don't need to include `normalize.css` or `sanitize.css` anymore.

Bamboo CSS uses CSS variables for theming, allowing to dynamically change the theme if you want. By default, it provides 2 themes for light and dark modes. The theme is automatically switched based on the system mode.

All CSS variables are prefixed with `--b-`, allowing to use Bamboo CSS with any CSS framework without conflicts.

Bamboo CSS is very lightweight, only **1.67KB** (minified and gzipped).

[View demo](https://rilwis.github.io/bamboo/demo/)

[Learn more why I create Bamboo CSS](https://deluxeblogtips.com/bamboo-css/)

## Features:

- Drop in to use, no configuration, no CSS classes
- Consistent styling across browsers thanks to `modern-normalize` and `sanitize.css`
- Responsive
- Supports light and dark themes (automatically detect the OS mode and switch)
- Uses CSS variables (scoped with prefix `--b-`)
- Uses `rem`
- Compatible with other CSS frameworks
- Lightweight

## Usage

### CDN

#### 🌙/☀ Automatic Theme

```html
<link rel="stylesheet" href="https://unpkg.com/bamboo.css">
```

#### 🌙 Dark Theme

```html
<link rel="stylesheet" href="https://unpkg.com/bamboo.css/dist/dark.min.css">
```

#### ☀ Light Theme

```html
<link rel="stylesheet" href="https://unpkg.com/bamboo.css/dist/light.min.css">
```

### NPM

```bash
npm install --save bamboo.css
```

### Download

See [https://rilwis.github.io/bamboo/dist/bamboo.min.css](https://rilwis.github.io/bamboo/dist/bamboo.min.css)

### Theming

Bamboo CSS provides the following CSS variables for theming:

```css
:root {
	--b-font-main: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Oxygen-Sans, Ubuntu, Cantarell, "Helvetica Neue", sans-serif;
	--b-font-mono: Consolas, Monaco, monospace;

	--b-txt: #2e3440;
	--b-bg-1: #fff;
	--b-bg-2: #eceff4;
	--b-line: #eceff4;
	--b-link: #bf616a;
	--b-btn-bg: #242933;
	--b-btn-txt: #fff;
	--b-focus: #d8dee9;
}
```

All CSS variables are prefixed with `--b-` so it's safe to use Bamboo CSS with your existing websites.

## Credits
- [modern-normalize](https://github.com/sindresorhus/modern-normalize) and [sanitize.css](https://github.com/csstools/sanitize.css)
- [Nord theme](https://www.nordtheme.com) for color schemes
