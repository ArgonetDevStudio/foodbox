package shanepark.foodbox.image.domain;

import lombok.Getter;
import shanepark.foodbox.api.domain.Menu;

import java.time.Clock;
import java.time.DateTimeException;
import java.time.LocalDate;
import java.time.temporal.ChronoUnit;
import java.util.ArrayList;
import java.util.List;
import java.util.Objects;
import java.util.regex.Matcher;
import java.util.regex.Pattern;
import java.util.stream.Stream;

@Getter
public class ParsedMenu {
    private final LocalDate date;
    private final List<String> menus = new ArrayList<>();
    private final Pattern pattern = Pattern.compile("[월화수목금토일]\\(([A-Z]{3})\\)(\\d{1,2})일");

    public ParsedMenu(Clock clock, String dateStr) {
        Matcher matcher = pattern.matcher(dateStr);
        if (!matcher.matches()) {
            throw new IllegalArgumentException("날짜 형식이 잘못되었습니다: " + dateStr);
        }

        String dayText = matcher.group(1);
        int day = Integer.parseInt(matcher.group(2));

        LocalDate today = LocalDate.now(clock);
        List<LocalDate> candidates = Stream.of(
                buildDateSafe(today, -1, day),
                buildDateSafe(today, 0, day),
                buildDateSafe(today, 1, day)
        ).filter(Objects::nonNull).toList();

        for (LocalDate candidate : candidates) {
            String dayOfWeek = candidate.getDayOfWeek().name();
            if (!dayOfWeek.startsWith(dayText)) {
                continue;
            }
            long dayDiff = Math.abs(ChronoUnit.DAYS.between(today, candidate));
            if (14 < dayDiff)
                continue;
            this.date = candidate;
            return;
        }

        throw new IllegalArgumentException("날짜 계산이 불가능 합니다: " + dateStr);
    }

    private LocalDate buildDateSafe(LocalDate today, int monthOffset, int day) {
        try {
            return today.withDayOfMonth(1)
                    .plusMonths(monthOffset)
                    .withDayOfMonth(day);
        } catch (DateTimeException e) {
            return null;
        }
    }


    public void setMenu(String menu) {
        for (String m : menu.split("\n")) {
            m = m.trim();
            if (m.isEmpty())
                continue;
            menus.add(m);
        }

        if (!menus.isEmpty()) {
            highlightSalad();
        }
    }

    private void highlightSalad() {
        String lastMenu = menus.removeLast();
        menus.add(String.format("[%s]", lastMenu));
    }

    @Override
    public String toString() {
        return String.format("<%s>\n%s\n", date, menus);
    }

    public Menu toMenuResponse() {
        return new Menu(date, menus);
    }
}
