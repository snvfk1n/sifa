// env
// POSTMARK_API_KEY
// SECRET

// read config.json into targets struct

// use gorm as ORM: https://gorm.io/docs/models.html

// /POST webhook/:id (x-secret: <abc>)
// if id doesn't exist, reply with 404
// db.put(id, timestamp), reply with 200
//
// also, some kind of daily (?) cron job that checks the latest target call
// could use cron syntax https://github.com/adhocore/gronx
// if target is due
//   send email via postmark:
//
//   Hello, the target "$TARGET_NAME" hasn't been called in the required amount of time. Please check your systems! Last call was: $TARGET_LAST_CALL.
//
