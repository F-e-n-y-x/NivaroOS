package com.fenyx.nivaroos_mobile

import android.Manifest
import android.content.ContentUris
import android.content.ContentValues
import android.content.Context
import android.content.pm.ApplicationInfo
import android.content.pm.PackageManager
import android.database.Cursor
import android.media.MediaScannerConnection
import android.net.Uri
import android.os.Build
import android.os.Environment
import android.provider.CalendarContract
import android.provider.CallLog
import android.provider.ContactsContract
import android.provider.DocumentsContract
import android.provider.MediaStore
import android.provider.Settings
import android.provider.Telephony
import android.util.Xml
import android.webkit.MimeTypeMap
import org.xmlpull.v1.XmlPullParser
import java.io.File
import java.io.FileInputStream
import java.io.FileOutputStream
import java.io.InputStream
import java.io.RandomAccessFile
import java.security.MessageDigest
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import java.util.TimeZone

/**
 * What "Back up this phone" reads from Android, and what a restore writes
 * back (lib/phone_backup/pb_platform.dart). Everything here runs off the
 * main thread (PhoneBackupBridge). A refused permission surfaces as a
 * SecurityException, which the Dart side turns into "allow access".
 *
 * Only framework APIs: no androidx on the app's classpath.
 */
object PhoneBackupData {

    // ---------------------------------------------------------------- media

    private fun Cursor.str(col: String): String? {
        val i = getColumnIndex(col)
        return if (i < 0 || isNull(i)) null else getString(i)
    }

    private fun Cursor.long(col: String): Long {
        val i = getColumnIndex(col)
        return if (i < 0 || isNull(i)) 0L else getLong(i)
    }

    /** Photos and videos on every volume; path = RELATIVE_PATH + DISPLAY_NAME (other volumes prefixed with their name). */
    fun scanMedia(ctx: Context): List<Map<String, Any?>> {
        val out = ArrayList<Map<String, Any?>>()
        val cr = ctx.contentResolver
        val files = if (Build.VERSION.SDK_INT >= 29) MediaStore.Files.getContentUri(MediaStore.VOLUME_EXTERNAL) else MediaStore.Files.getContentUri("external")
        val proj = ArrayList(listOf(
            MediaStore.Files.FileColumns._ID,
            MediaStore.Files.FileColumns.DISPLAY_NAME,
            MediaStore.Files.FileColumns.SIZE,
            MediaStore.Files.FileColumns.DATE_MODIFIED,
            MediaStore.Files.FileColumns.MEDIA_TYPE,
        ))
        @Suppress("DEPRECATION")
        proj.add(MediaStore.Files.FileColumns.DATA)
        if (Build.VERSION.SDK_INT >= 29) {
            proj.add(MediaStore.Files.FileColumns.RELATIVE_PATH)
            proj.add(MediaStore.Files.FileColumns.VOLUME_NAME)
            proj.add(MediaStore.MediaColumns.DATE_TAKEN)
        }
        var sel = "${MediaStore.Files.FileColumns.MEDIA_TYPE} IN (${MediaStore.Files.FileColumns.MEDIA_TYPE_IMAGE}, ${MediaStore.Files.FileColumns.MEDIA_TYPE_VIDEO})"
        if (Build.VERSION.SDK_INT >= 29) sel += " AND ${MediaStore.MediaColumns.IS_PENDING} = 0"
        val root = Environment.getExternalStorageDirectory().absolutePath.trimEnd('/') + "/"
        cr.query(files, proj.toTypedArray(), sel, null, "${MediaStore.Files.FileColumns._ID} ASC")?.use { c ->
            while (c.moveToNext()) {
                val id = c.long(MediaStore.Files.FileColumns._ID)
                val name = c.str(MediaStore.Files.FileColumns.DISPLAY_NAME) ?: continue
                val type = c.long(MediaStore.Files.FileColumns.MEDIA_TYPE).toInt()
                val path: String
                val volume: String
                if (Build.VERSION.SDK_INT >= 29) {
                    volume = c.str(MediaStore.Files.FileColumns.VOLUME_NAME) ?: MediaStore.VOLUME_EXTERNAL_PRIMARY
                    val rel = (c.str(MediaStore.Files.FileColumns.RELATIVE_PATH) ?: "").trim('/')
                    val base = if (rel.isEmpty()) name else "$rel/$name"
                    path = if (volume == MediaStore.VOLUME_EXTERNAL_PRIMARY) base else "$volume/$base"
                } else {
                    volume = "external"
                    @Suppress("DEPRECATION")
                    val data = c.str(MediaStore.Files.FileColumns.DATA) ?: continue
                    path = if (data.startsWith(root)) data.substring(root.length) else data.trimStart('/')
                }
                val collection = if (type == MediaStore.Files.FileColumns.MEDIA_TYPE_VIDEO) {
                    if (Build.VERSION.SDK_INT >= 29) MediaStore.Video.Media.getContentUri(volume) else MediaStore.Video.Media.EXTERNAL_CONTENT_URI
                } else {
                    if (Build.VERSION.SDK_INT >= 29) MediaStore.Images.Media.getContentUri(volume) else MediaStore.Images.Media.EXTERNAL_CONTENT_URI
                }
                out.add(mapOf(
                    "id" to id,
                    "uri" to ContentUris.withAppendedId(collection, id).toString(),
                    "path" to path,
                    "size" to c.long(MediaStore.Files.FileColumns.SIZE),
                    "mtime" to c.long(MediaStore.Files.FileColumns.DATE_MODIFIED) * 1000L,
                    "taken_at" to if (Build.VERSION.SDK_INT >= 29) c.long(MediaStore.MediaColumns.DATE_TAKEN) else 0L,
                ))
            }
        }
        return out
    }

    // ------------------------------------------------------------ SAF trees

    fun scanTree(ctx: Context, tree: String, label: String): List<Map<String, Any?>> {
        val treeUri = Uri.parse(tree)
        val out = ArrayList<Map<String, Any?>>()
        val cr = ctx.contentResolver
        val proj = arrayOf(
            DocumentsContract.Document.COLUMN_DOCUMENT_ID,
            DocumentsContract.Document.COLUMN_DISPLAY_NAME,
            DocumentsContract.Document.COLUMN_MIME_TYPE,
            DocumentsContract.Document.COLUMN_SIZE,
            DocumentsContract.Document.COLUMN_LAST_MODIFIED,
        )
        val stack = ArrayDeque<Pair<String, String>>()
        stack.add(DocumentsContract.getTreeDocumentId(treeUri) to label)
        while (stack.isNotEmpty() && out.size < 500_000) {
            val (docId, prefix) = stack.removeLast()
            val children = DocumentsContract.buildChildDocumentsUriUsingTree(treeUri, docId)
            cr.query(children, proj, null, null, null)?.use { c ->
                while (c.moveToNext()) {
                    val id = c.getString(0) ?: continue
                    val name = c.getString(1) ?: continue
                    val mime = c.getString(2) ?: ""
                    val path = "$prefix/${name.replace('/', '_')}"
                    if (mime == DocumentsContract.Document.MIME_TYPE_DIR) {
                        if (prefix.count { it == '/' } < 64) stack.add(id to path)
                    } else {
                        out.add(mapOf(
                            "uri" to DocumentsContract.buildDocumentUriUsingTree(treeUri, id).toString(),
                            "path" to path,
                            "size" to (if (c.isNull(3)) 0L else c.getLong(3)),
                            "mtime" to (if (c.isNull(4)) 0L else c.getLong(4)),
                        ))
                    }
                }
            }
        }
        return out
    }

    fun treeLabel(ctx: Context, tree: Uri): String {
        return try {
            val doc = DocumentsContract.buildDocumentUriUsingTree(tree, DocumentsContract.getTreeDocumentId(tree))
            ctx.contentResolver.query(doc, arrayOf(DocumentsContract.Document.COLUMN_DISPLAY_NAME), null, null, null)?.use { c ->
                if (c.moveToFirst()) c.getString(0) else null
            } ?: "Folder"
        } catch (e: Exception) {
            "Folder"
        }
    }

    // ------------------------------------------------------ reading sources

    private fun hasMediaLocation(ctx: Context): Boolean =
        Build.VERSION.SDK_INT >= 29 && ctx.checkSelfPermission(Manifest.permission.ACCESS_MEDIA_LOCATION) == PackageManager.PERMISSION_GRANTED

    /** A media URI asks for the original (with the photo's location) when the app may have it. */
    private fun sourceUri(ctx: Context, source: String): Uri {
        val uri = Uri.parse(source)
        if (uri.authority == MediaStore.AUTHORITY && hasMediaLocation(ctx)) {
            return try { MediaStore.setRequireOriginal(uri) } catch (e: Exception) { uri }
        }
        return uri
    }

    private fun openStream(ctx: Context, source: String): InputStream =
        if (source.startsWith("/")) FileInputStream(source)
        else ctx.contentResolver.openInputStream(sourceUri(ctx, source)) ?: throw java.io.FileNotFoundException(source)

    fun sha256(ctx: Context, source: String): String {
        val md = MessageDigest.getInstance("SHA-256")
        openStream(ctx, source).use { input ->
            val buf = ByteArray(1 shl 16)
            while (true) {
                val n = input.read(buf)
                if (n < 0) break
                md.update(buf, 0, n)
            }
        }
        return md.digest().joinToString("") { "%02x".format(it) }
    }

    fun read(ctx: Context, source: String, offset: Long, length: Int): ByteArray {
        if (source.startsWith("/")) {
            RandomAccessFile(source, "r").use { f ->
                f.seek(offset)
                return readFully(length) { b, o, l -> f.read(b, o, l) }
            }
        }
        val uri = sourceUri(ctx, source)
        try {
            ctx.contentResolver.openFileDescriptor(uri, "r")?.use { pfd ->
                FileInputStream(pfd.fileDescriptor).use { s ->
                    s.channel.position(offset)
                    return readFully(length) { b, o, l -> s.read(b, o, l) }
                }
            }
        } catch (e: SecurityException) {
            throw e
        } catch (e: Exception) {
            // Some providers only stream: skip to the offset.
        }
        openStream(ctx, source).use { s ->
            var skip = offset
            while (skip > 0) {
                val n = s.skip(skip)
                if (n <= 0) break
                skip -= n
            }
            return readFully(length) { b, o, l -> s.read(b, o, l) }
        }
    }

    private inline fun readFully(length: Int, read: (ByteArray, Int, Int) -> Int): ByteArray {
        val buf = ByteArray(length)
        var got = 0
        while (got < length) {
            val n = read(buf, got, length - got)
            if (n < 0) break
            got += n
        }
        return if (got == length) buf else buf.copyOf(got)
    }

    // ------------------------------------------------------------- contacts

    /** Every contact in one vCard file; null when there are none. */
    fun exportContacts(ctx: Context, dir: String): String? {
        val cr = ctx.contentResolver
        val keys = ArrayList<String>()
        cr.query(ContactsContract.Contacts.CONTENT_URI, arrayOf(ContactsContract.Contacts.LOOKUP_KEY), null, null, ContactsContract.Contacts._ID + " ASC")?.use { c ->
            while (c.moveToNext()) c.getString(0)?.let { keys.add(it) }
        }
        if (keys.isEmpty()) return null
        val out = File(dir, "contacts.vcf")
        FileOutputStream(out).use { sink ->
            for (batch in keys.chunked(50)) {
                val ok = try {
                    val uri = Uri.withAppendedPath(ContactsContract.Contacts.CONTENT_MULTI_VCARD_URI, Uri.encode(batch.joinToString(":")))
                    cr.openAssetFileDescriptor(uri, "r")?.use { fd -> fd.createInputStream().use { it.copyTo(sink) } }
                    true
                } catch (e: SecurityException) {
                    throw e
                } catch (e: Exception) {
                    false
                }
                if (!ok) {
                    // One at a time for providers without multi-vCard.
                    for (k in batch) {
                        try {
                            val uri = Uri.withAppendedPath(ContactsContract.Contacts.CONTENT_VCARD_URI, k)
                            cr.openAssetFileDescriptor(uri, "r")?.use { fd -> fd.createInputStream().use { it.copyTo(sink) } }
                        } catch (e: SecurityException) {
                            throw e
                        } catch (_: Exception) {
                        }
                    }
                }
            }
        }
        return out.absolutePath
    }

    // ------------------------------------------------------------- calendar

    private val icsUtc = SimpleDateFormat("yyyyMMdd'T'HHmmss'Z'", Locale.US).apply { timeZone = TimeZone.getTimeZone("UTC") }
    private val icsDate = SimpleDateFormat("yyyyMMdd", Locale.US).apply { timeZone = TimeZone.getTimeZone("UTC") }

    private fun icsText(s: String): String = s.replace("\\", "\\\\").replace(";", "\\;").replace(",", "\\,").replace("\r\n", "\\n").replace("\n", "\\n").replace("\r", "\\n")

    /** RFC 5545 folding: lines of at most 75 octets, continued with a space. */
    private fun fold(line: String, sb: StringBuilder) {
        val bytes = line.toByteArray(Charsets.UTF_8)
        if (bytes.size <= 75) {
            sb.append(line).append("\r\n")
            return
        }
        var count = 0
        var first = true
        val cur = StringBuilder()
        var i = 0
        while (i < line.length) {
            val cp = line.codePointAt(i)
            val chars = Character.charCount(cp)
            val s = line.substring(i, i + chars)
            val n = s.toByteArray(Charsets.UTF_8).size
            val limit = if (first) 75 else 74
            if (count + n > limit) {
                sb.append(cur).append("\r\n ")
                cur.setLength(0)
                count = 0
                first = false
            }
            cur.append(s)
            count += n
            i += chars
        }
        sb.append(cur).append("\r\n")
    }

    /** One .ics file per calendar: {name, path, events}. */
    fun exportCalendars(ctx: Context, dir: String): List<Map<String, Any?>> {
        val cr = ctx.contentResolver
        val out = ArrayList<Map<String, Any?>>()
        val cals = ArrayList<Pair<Long, String>>()
        cr.query(CalendarContract.Calendars.CONTENT_URI, arrayOf(CalendarContract.Calendars._ID, CalendarContract.Calendars.CALENDAR_DISPLAY_NAME, CalendarContract.Calendars.ACCOUNT_NAME), null, null, null)?.use { c ->
            while (c.moveToNext()) {
                val name = c.getString(1) ?: c.getString(2) ?: "Calendar"
                cals.add(c.getLong(0) to name)
            }
        }
        val stamp = icsUtc.format(Date())
        for ((id, name) in cals) {
            val sb = StringBuilder()
            sb.append("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//NivaroOS//Phone backup//EN\r\n")
            fold("X-WR-CALNAME:${icsText(name)}", sb)
            var events = 0
            val proj = arrayOf(
                CalendarContract.Events._ID, CalendarContract.Events.TITLE, CalendarContract.Events.DESCRIPTION,
                CalendarContract.Events.EVENT_LOCATION, CalendarContract.Events.DTSTART, CalendarContract.Events.DTEND,
                CalendarContract.Events.DURATION, CalendarContract.Events.ALL_DAY, CalendarContract.Events.RRULE,
                CalendarContract.Events.EXDATE, CalendarContract.Events.EVENT_TIMEZONE, CalendarContract.Events.UID_2445,
                CalendarContract.Events.RDATE,
            )
            cr.query(CalendarContract.Events.CONTENT_URI, proj, "${CalendarContract.Events.CALENDAR_ID} = ? AND ${CalendarContract.Events.DELETED} = 0", arrayOf(id.toString()), null)?.use { c ->
                while (c.moveToNext()) {
                    val eid = c.getLong(0)
                    val allDay = !c.isNull(7) && c.getInt(7) == 1
                    val start = if (c.isNull(4)) continue else c.getLong(4)
                    sb.append("BEGIN:VEVENT\r\n")
                    val uid = c.getString(11)?.takeIf { it.isNotBlank() } ?: "nivaroos-$id-$eid"
                    fold("UID:${icsText(uid)}", sb)
                    sb.append("DTSTAMP:$stamp\r\n")
                    if (allDay) sb.append("DTSTART;VALUE=DATE:${icsDate.format(Date(start))}\r\n") else sb.append("DTSTART:${icsUtc.format(Date(start))}\r\n")
                    if (!c.isNull(5)) {
                        val end = c.getLong(5)
                        if (allDay) sb.append("DTEND;VALUE=DATE:${icsDate.format(Date(end))}\r\n") else sb.append("DTEND:${icsUtc.format(Date(end))}\r\n")
                    } else if (!c.getString(6).isNullOrBlank()) {
                        sb.append("DURATION:${c.getString(6)}\r\n")
                    }
                    c.getString(1)?.let { fold("SUMMARY:${icsText(it)}", sb) }
                    c.getString(2)?.takeIf { it.isNotEmpty() }?.let { fold("DESCRIPTION:${icsText(it)}", sb) }
                    c.getString(3)?.takeIf { it.isNotEmpty() }?.let { fold("LOCATION:${icsText(it)}", sb) }
                    c.getString(8)?.takeIf { it.isNotBlank() }?.let { fold("RRULE:$it", sb) }
                    c.getString(12)?.takeIf { it.isNotBlank() }?.let { fold("RDATE:$it", sb) }
                    c.getString(9)?.takeIf { it.isNotBlank() }?.let { fold("EXDATE:$it", sb) }
                    // Reminders.
                    try {
                        cr.query(CalendarContract.Reminders.CONTENT_URI, arrayOf(CalendarContract.Reminders.MINUTES), "${CalendarContract.Reminders.EVENT_ID} = ?", arrayOf(eid.toString()), null)?.use { r ->
                            while (r.moveToNext()) {
                                val m = r.getInt(0)
                                if (m >= 0) sb.append("BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:Reminder\r\nTRIGGER:-PT${m}M\r\nEND:VALARM\r\n")
                            }
                        }
                    } catch (_: Exception) {
                    }
                    sb.append("END:VEVENT\r\n")
                    events++
                }
            }
            sb.append("END:VCALENDAR\r\n")
            val f = File(dir, "calendar-$id.ics")
            f.writeText(sb.toString(), Charsets.UTF_8)
            out.add(mapOf("name" to name, "path" to f.absolutePath, "events" to events))
        }
        return out
    }

    // ------------------------------------------------- messages and calls

    private fun rows(ctx: Context, uri: Uri, cols: List<String>, offset: Int, limit: Int, sort: String = "_id ASC"): List<Map<String, Any?>> {
        val out = ArrayList<Map<String, Any?>>()
        ctx.contentResolver.query(uri, null, null, null, sort)?.use { c ->
            if (!c.moveToPosition(offset)) return out
            do {
                val m = HashMap<String, Any?>()
                for (col in cols) {
                    val i = c.getColumnIndex(col)
                    if (i < 0 || c.isNull(i)) {
                        m[col] = null
                    } else when (c.getType(i)) {
                        Cursor.FIELD_TYPE_INTEGER -> m[col] = c.getLong(i)
                        Cursor.FIELD_TYPE_FLOAT -> m[col] = c.getDouble(i)
                        Cursor.FIELD_TYPE_BLOB -> m[col] = null
                        else -> m[col] = c.getString(i)
                    }
                }
                out.add(m)
            } while (out.size < limit && c.moveToNext())
        }
        return out
    }

    fun readSms(ctx: Context, offset: Int, limit: Int) = rows(
        ctx, Telephony.Sms.CONTENT_URI,
        listOf("_id", "address", "date", "date_sent", "type", "body", "read", "status", "subject", "service_center", "protocol", "locked", "sub_id"),
        offset, limit,
    )

    fun readMms(ctx: Context, offset: Int, limit: Int): List<Map<String, Any?>> {
        val list = rows(
            ctx, Telephony.Mms.CONTENT_URI,
            listOf("_id", "date", "date_sent", "msg_box", "m_id", "sub", "ct_t", "read", "m_type", "text_only", "sub_id", "locked"),
            offset, limit,
        )
        return list.map { m ->
            val id = (m["_id"] as? Long) ?: return@map m
            val addrs = ArrayList<Map<String, Any?>>()
            try {
                ctx.contentResolver.query(Uri.parse("content://mms/$id/addr"), arrayOf("address", "type", "charset"), null, null, null)?.use { c ->
                    while (c.moveToNext()) addrs.add(mapOf("address" to c.getString(0), "type" to c.getLong(1), "charset" to (if (c.isNull(2)) 106L else c.getLong(2))))
                }
            } catch (_: Exception) {
            }
            HashMap(m).apply { put("addrs", addrs) }
        }
    }

    /** The parts of one MMS; attachments' bytes up to [maxBytes] in all. */
    fun mmsParts(ctx: Context, id: Long, maxBytes: Int): List<Map<String, Any?>> {
        val out = ArrayList<Map<String, Any?>>()
        var budget = maxBytes
        ctx.contentResolver.query(Uri.parse("content://mms/part"), null, "mid = ?", arrayOf(id.toString()), "seq ASC")?.use { c ->
            while (c.moveToNext()) {
                val m = HashMap<String, Any?>()
                for (col in listOf("seq", "ct", "name", "chset", "cd", "fn", "cid", "cl", "text")) {
                    val i = c.getColumnIndex(col)
                    m[col] = if (i < 0 || c.isNull(i)) null else c.getString(i)
                }
                val pid = c.getLong(c.getColumnIndex("_id"))
                val ct = (m["ct"] as? String) ?: ""
                val dataCol = c.getColumnIndex("_data")
                val hasData = dataCol >= 0 && !c.isNull(dataCol)
                if (hasData && ct != "text/plain" && ct != "application/smil" && budget > 0) {
                    try {
                        ctx.contentResolver.openInputStream(Uri.parse("content://mms/part/$pid"))?.use { s ->
                            val bytes = s.readBytes()
                            if (bytes.size <= budget) {
                                m["data"] = bytes
                                budget -= bytes.size
                            }
                        }
                    } catch (_: Exception) {
                    }
                }
                out.add(m)
            }
        }
        return out
    }

    fun readCallLog(ctx: Context, offset: Int, limit: Int) = rows(
        ctx, CallLog.Calls.CONTENT_URI,
        listOf("_id", "number", "duration", "date", "type", "presentation", "subscription_id", "post_dial_digits", "name"),
        offset, limit,
    )

    // ----------------------------------------------------------------- apps

    @Suppress("DEPRECATION")
    fun installedApps(ctx: Context): List<Map<String, Any?>> {
        val pm = ctx.packageManager
        val out = ArrayList<Map<String, Any?>>()
        for (p in pm.getInstalledPackages(0)) {
            val ai = p.applicationInfo ?: continue
            val system = (ai.flags and ApplicationInfo.FLAG_SYSTEM) != 0
            val apks = ArrayList<Map<String, Any?>>()
            var size = 0L
            fun add(path: String?) {
                if (path == null) return
                val f = File(path)
                if (!f.canRead()) return
                apks.add(mapOf("name" to f.name, "path" to path, "size" to f.length(), "mtime" to f.lastModified()))
                size += f.length()
            }
            add(ai.sourceDir)
            ai.splitSourceDirs?.forEach { add(it) }
            val installer = try {
                if (Build.VERSION.SDK_INT >= 30) pm.getInstallSourceInfo(p.packageName).installingPackageName else pm.getInstallerPackageName(p.packageName)
            } catch (e: Exception) {
                null
            }
            out.add(mapOf(
                "package" to p.packageName,
                "label" to (try { pm.getApplicationLabel(ai).toString() } catch (e: Exception) { p.packageName }),
                "version_name" to p.versionName,
                "version_code" to if (Build.VERSION.SDK_INT >= 28) p.longVersionCode else p.versionCode.toLong(),
                "system" to system,
                "installer" to installer,
                "first_install" to p.firstInstallTime,
                "last_update" to p.lastUpdateTime,
                "apks" to apks,
                "size" to size,
            ))
        }
        return out
    }

    // ------------------------------------------------------------- settings

    fun readSettings(ctx: Context): Map<String, Any?> {
        val cr = ctx.contentResolver
        val sys = HashMap<String, Any?>()
        for (k in listOf(
            Settings.System.SCREEN_OFF_TIMEOUT, Settings.System.SCREEN_BRIGHTNESS, Settings.System.SCREEN_BRIGHTNESS_MODE,
            Settings.System.FONT_SCALE, Settings.System.HAPTIC_FEEDBACK_ENABLED, Settings.System.SOUND_EFFECTS_ENABLED,
            Settings.System.RINGTONE, Settings.System.NOTIFICATION_SOUND, Settings.System.ALARM_ALERT, Settings.System.TIME_12_24,
            Settings.System.ACCELEROMETER_ROTATION, Settings.System.DTMF_TONE_WHEN_DIALING,
        )) {
            try { sys[k] = Settings.System.getString(cr, k) } catch (_: Exception) {}
        }
        val global = HashMap<String, Any?>()
        for (k in listOf(Settings.Global.AUTO_TIME, Settings.Global.AUTO_TIME_ZONE, "device_name")) {
            try { global[k] = Settings.Global.getString(cr, k) } catch (_: Exception) {}
        }
        val dm = ctx.resources.displayMetrics
        return mapOf(
            "taken_at" to System.currentTimeMillis(),
            "device" to mapOf(
                "manufacturer" to Build.MANUFACTURER, "model" to Build.MODEL, "android" to Build.VERSION.RELEASE,
                "sdk" to Build.VERSION.SDK_INT, "security_patch" to (if (Build.VERSION.SDK_INT >= 23) Build.VERSION.SECURITY_PATCH else ""),
                "density_dpi" to dm.densityDpi,
            ),
            "locale" to Locale.getDefault().toLanguageTag(),
            "time_zone" to TimeZone.getDefault().id,
            "system" to sys,
            "global" to global,
        )
    }

    // -------------------------------------------------------------- restore

    private fun mimeOf(name: String): String {
        val ext = name.substringAfterLast('.', "").lowercase(Locale.US)
        return MimeTypeMap.getSingleton().getMimeTypeFromExtension(ext) ?: "application/octet-stream"
    }

    private fun copyInto(ctx: Context, tmp: String, target: Uri) {
        ctx.contentResolver.openOutputStream(target, "w")?.use { out -> FileInputStream(tmp).use { it.copyTo(out) } }
            ?: throw java.io.IOException("Can't write $target")
    }

    /** A restored photo or video back into MediaStore at its folder (or Pictures/Restored when that folder isn't allowed). */
    fun saveToMedia(ctx: Context, tmp: String, relativePath: String, name: String, takenAt: Long, mtime: Long): String {
        val mime = mimeOf(name)
        val video = mime.startsWith("video/")
        if (Build.VERSION.SDK_INT >= 29) {
            val collection = if (video) MediaStore.Video.Media.getContentUri(MediaStore.VOLUME_EXTERNAL_PRIMARY) else MediaStore.Images.Media.getContentUri(MediaStore.VOLUME_EXTERNAL_PRIMARY)
            fun insert(rel: String): Uri? {
                val v = ContentValues().apply {
                    put(MediaStore.MediaColumns.DISPLAY_NAME, name)
                    put(MediaStore.MediaColumns.MIME_TYPE, mime)
                    put(MediaStore.MediaColumns.RELATIVE_PATH, rel)
                    put(MediaStore.MediaColumns.IS_PENDING, 1)
                    if (takenAt > 0) put(MediaStore.MediaColumns.DATE_TAKEN, takenAt)
                }
                return try { ctx.contentResolver.insert(collection, v) } catch (e: IllegalArgumentException) { null }
            }
            val rel = relativePath.trim('/').let { if (it.isEmpty()) "" else "$it/" }
            val uri = insert(rel) ?: insert(if (video) "Movies/Restored/" else "Pictures/Restored/") ?: throw java.io.IOException("MediaStore refused $name")
            try {
                copyInto(ctx, tmp, uri)
                ctx.contentResolver.update(uri, ContentValues().apply {
                    put(MediaStore.MediaColumns.IS_PENDING, 0)
                    if (mtime > 0) put(MediaStore.MediaColumns.DATE_MODIFIED, mtime / 1000)
                }, null, null)
            } catch (e: Exception) {
                ctx.contentResolver.delete(uri, null, null)
                throw e
            }
            return uri.toString()
        }
        @Suppress("DEPRECATION")
        val dir = File(Environment.getExternalStorageDirectory(), relativePath.trim('/'))
        dir.mkdirs()
        val f = File(dir, name)
        FileInputStream(tmp).use { input -> FileOutputStream(f).use { input.copyTo(it) } }
        if (mtime > 0) f.setLastModified(mtime)
        MediaScannerConnection.scanFile(ctx, arrayOf(f.absolutePath), arrayOf(mime), null)
        return Uri.fromFile(f).toString()
    }

    fun mediaExists(ctx: Context, relativePath: String, name: String, size: Long): Boolean {
        if (Build.VERSION.SDK_INT < 29) {
            @Suppress("DEPRECATION")
            val f = File(File(Environment.getExternalStorageDirectory(), relativePath.trim('/')), name)
            return f.exists() && f.length() == size
        }
        val rel = relativePath.trim('/').let { if (it.isEmpty()) "" else "$it/" }
        ctx.contentResolver.query(
            MediaStore.Files.getContentUri(MediaStore.VOLUME_EXTERNAL),
            arrayOf(MediaStore.MediaColumns.SIZE),
            "${MediaStore.MediaColumns.RELATIVE_PATH} = ? AND ${MediaStore.MediaColumns.DISPLAY_NAME} = ?",
            arrayOf(rel, name), null,
        )?.use { c ->
            while (c.moveToNext()) if (c.getLong(0) == size) return true
        }
        return false
    }

    /** A restored file into Download/NivaroOS restore/[subPath]. */
    fun saveToDownloads(ctx: Context, tmp: String, subPath: String, name: String): String {
        val sub = subPath.trim('/')
        if (Build.VERSION.SDK_INT >= 29) {
            val v = ContentValues().apply {
                put(MediaStore.MediaColumns.DISPLAY_NAME, name)
                put(MediaStore.MediaColumns.MIME_TYPE, mimeOf(name))
                put(MediaStore.MediaColumns.RELATIVE_PATH, "${Environment.DIRECTORY_DOWNLOADS}/NivaroOS restore/${if (sub.isEmpty()) "" else "$sub/"}")
                put(MediaStore.MediaColumns.IS_PENDING, 1)
            }
            val uri = ctx.contentResolver.insert(MediaStore.Downloads.EXTERNAL_CONTENT_URI, v) ?: throw java.io.IOException("Downloads refused $name")
            copyInto(ctx, tmp, uri)
            ctx.contentResolver.update(uri, ContentValues().apply { put(MediaStore.MediaColumns.IS_PENDING, 0) }, null, null)
            return uri.toString()
        }
        @Suppress("DEPRECATION")
        val dir = File(Environment.getExternalStoragePublicDirectory(Environment.DIRECTORY_DOWNLOADS), "NivaroOS restore/$sub")
        dir.mkdirs()
        val f = File(dir, name)
        FileInputStream(tmp).use { input -> FileOutputStream(f).use { input.copyTo(it) } }
        return Uri.fromFile(f).toString()
    }

    /** SMS Backup & Restore messages into the SMS provider (the app must be the default SMS app). MMS are counted, not written. */
    fun restoreSms(ctx: Context, path: String): Map<String, Int> {
        val cr = ctx.contentResolver
        var inserted = 0
        var skipped = 0
        var mms = 0
        var failed = 0
        FileInputStream(path).use { input ->
            val p = Xml.newPullParser()
            p.setFeature(XmlPullParser.FEATURE_PROCESS_NAMESPACES, false)
            p.setInput(input, "UTF-8")
            while (p.next() != XmlPullParser.END_DOCUMENT) {
                if (p.eventType != XmlPullParser.START_TAG) continue
                when (p.name) {
                    "sms" -> {
                        fun a(n: String) = p.getAttributeValue(null, n)?.takeIf { it != "null" }
                        val address = a("address") ?: ""
                        val date = a("date") ?: continue
                        val type = a("type") ?: "1"
                        val body = a("body") ?: ""
                        val exists = try {
                            cr.query(Telephony.Sms.CONTENT_URI, arrayOf("_id"), "address = ? AND date = ? AND type = ? AND body = ?", arrayOf(address, date, type, body), null)?.use { it.count > 0 } ?: false
                        } catch (e: Exception) {
                            false
                        }
                        if (exists) {
                            skipped++
                            continue
                        }
                        val v = ContentValues().apply {
                            put("address", address)
                            put("date", date.toLongOrNull() ?: 0L)
                            a("date_sent")?.toLongOrNull()?.let { put("date_sent", it) }
                            put("type", type.toIntOrNull() ?: 1)
                            put("body", body)
                            put("read", a("read")?.toIntOrNull() ?: 1)
                            put("seen", 1)
                            a("status")?.toIntOrNull()?.let { put("status", it) }
                            a("subject")?.let { put("subject", it) }
                            a("service_center")?.let { put("service_center", it) }
                            a("protocol")?.toIntOrNull()?.let { put("protocol", it) }
                            put("locked", a("locked")?.toIntOrNull() ?: 0)
                        }
                        try {
                            if (cr.insert(Telephony.Sms.CONTENT_URI, v) != null) inserted++ else failed++
                        } catch (e: Exception) {
                            failed++
                        }
                    }
                    "mms" -> mms++
                }
            }
        }
        return mapOf("inserted" to inserted, "skipped" to skipped, "mms" to mms, "failed" to failed)
    }

    fun restoreCallLog(ctx: Context, path: String): Map<String, Int> {
        val cr = ctx.contentResolver
        var inserted = 0
        var skipped = 0
        var failed = 0
        FileInputStream(path).use { input ->
            val p = Xml.newPullParser()
            p.setInput(input, "UTF-8")
            while (p.next() != XmlPullParser.END_DOCUMENT) {
                if (p.eventType != XmlPullParser.START_TAG || p.name != "call") continue
                fun a(n: String) = p.getAttributeValue(null, n)?.takeIf { it != "null" }
                val number = a("number") ?: ""
                val date = a("date") ?: continue
                val duration = a("duration") ?: "0"
                val exists = try {
                    cr.query(CallLog.Calls.CONTENT_URI, arrayOf("_id"), "${CallLog.Calls.NUMBER} = ? AND ${CallLog.Calls.DATE} = ? AND ${CallLog.Calls.DURATION} = ?", arrayOf(number, date, duration), null)?.use { it.count > 0 } ?: false
                } catch (e: SecurityException) {
                    throw e
                } catch (e: Exception) {
                    false
                }
                if (exists) {
                    skipped++
                    continue
                }
                val v = ContentValues().apply {
                    put(CallLog.Calls.NUMBER, number)
                    put(CallLog.Calls.DATE, date.toLongOrNull() ?: 0L)
                    put(CallLog.Calls.DURATION, duration.toLongOrNull() ?: 0L)
                    put(CallLog.Calls.TYPE, a("type")?.toIntOrNull() ?: CallLog.Calls.INCOMING_TYPE)
                    put(CallLog.Calls.NUMBER_PRESENTATION, a("presentation")?.toIntOrNull() ?: CallLog.Calls.PRESENTATION_ALLOWED)
                    put(CallLog.Calls.NEW, 0)
                    a("contact_name")?.takeIf { it != "(Unknown)" }?.let { put(CallLog.Calls.CACHED_NAME, it) }
                }
                try {
                    if (cr.insert(CallLog.Calls.CONTENT_URI, v) != null) inserted++ else failed++
                } catch (e: SecurityException) {
                    throw e
                } catch (e: Exception) {
                    failed++
                }
            }
        }
        return mapOf("inserted" to inserted, "skipped" to skipped, "failed" to failed)
    }
}
