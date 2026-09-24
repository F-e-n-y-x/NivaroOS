// DOM ids for wizard fields, so the error summary at the top of a step can
// link to each field and each field can point at its error with
// aria-describedby (spec §13 "Wizard errors").
export function fieldId(idp, field) {
	return `${idp}-${String(field).replace(/[^a-zA-Z0-9_-]/g, '-')}`
}

export function errorId(idp, field) {
	return `${fieldId(idp, field)}-err`
}

// describedBy joins the error id (when the field has an error) with any
// other ids (hints).
export function describedBy(idp, field, errors, ...more) {
	const ids = more.filter(Boolean)
	if (errors && errors[field]) ids.unshift(errorId(idp, field))
	return ids.length ? ids.join(' ') : null
}
