package shanepark.foodbox.slack.domain.enums;

import lombok.Getter;

import java.time.DayOfWeek;
import java.time.LocalDate;

import static java.time.DayOfWeek.*;

public enum NotifyDate {
    BENTO_DAY(""),
    SALAD_DAY("데니스델리 \uD83E\uDD57"),
    EATING_OUT_DAY("외식 \uD83C\uDF7D"),
    WEEKENDS("Today is weekends. Skip notifying today's menu."),
    ;

    @Getter
    private final String message;

    NotifyDate(String message) {
        this.message = message;
    }

    public static NotifyDate of(LocalDate date) {
        DayOfWeek dayOfWeek = date.getDayOfWeek();
        if (dayOfWeek == SATURDAY || dayOfWeek == SUNDAY) {
            return WEEKENDS;
        }
        if (dayOfWeek != WEDNESDAY)
            return BENTO_DAY;
        if (date.getMonth().length(date.isLeapYear()) - date.getDayOfMonth() >= 7) {
            return SALAD_DAY;
        }
        return EATING_OUT_DAY;
    }

}
