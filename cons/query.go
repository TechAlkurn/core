package cons

const QUERY_MESSAGE_UPDATE = " UPDATE `user_message` SET `text`=?, `is_updated`=? WHERE `message_id` = ? AND `created_at` = ? "
