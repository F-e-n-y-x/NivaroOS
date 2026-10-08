const files = import.meta.glob('./*.json', { eager: true, import: 'default' })
const langs = {}
for (const [path, messages] of Object.entries(files)) {
	langs[path.replace(/(\.\/|\.json$)/g, '').toLowerCase()] = messages
}

export default langs
