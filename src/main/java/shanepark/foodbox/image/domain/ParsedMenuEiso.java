package shanepark.foodbox.image.domain;

import lombok.Getter;
import shanepark.foodbox.api.domain.Menu;

import java.time.Clock;
import java.time.LocalDate;
import java.time.YearMonth;
import java.util.ArrayList;
import java.util.List;
import java.util.regex.Matcher;
import java.util.regex.Pattern;

@Getter
public class ParsedMenuEiso {
    private final LocalDate date;
    private final List<String> menus = new ArrayList<>();
    private final Pattern pattern = Pattern.compile("(\\d{1,2})월\\s*(\\d{1,2})일");

    public ParsedMenuEiso(Clock clock, String dateStr) {
        dateStr = adjustDateString(dateStr);
        Matcher matcher = pattern.matcher(dateStr);
        if (!matcher.find()) {
            throw new IllegalArgumentException("날짜 형식이 잘못되었습니다: " + dateStr);
        }

        int month = Integer.parseInt(matcher.group(1));
        int day = Integer.parseInt(matcher.group(2));

        this.date = resolveDate(clock, month, day);
    }

    private String adjustDateString(String dateStr) {
        return dateStr.replaceAll("윌", "월").trim();
    }

    private LocalDate resolveDate(Clock clock, int month, int day) {
        LocalDate today = LocalDate.now(clock);
        YearMonth currentYearMonth = YearMonth.from(today);

        LocalDate candidate = LocalDate.of(currentYearMonth.getYear(), month, day);
        long diff = Math.abs(candidate.toEpochDay() - today.toEpochDay());
        if (diff <= 45) {
            return candidate;
        }

        LocalDate previousYearCandidate = LocalDate.of(currentYearMonth.getYear() - 1, month, day);
        LocalDate nextYearCandidate = LocalDate.of(currentYearMonth.getYear() + 1, month, day);

        LocalDate closestCandidate = candidate;
        long closestDiff = diff;

        long previousDiff = Math.abs(previousYearCandidate.toEpochDay() - today.toEpochDay());
        if (previousDiff < closestDiff) {
            closestCandidate = previousYearCandidate;
            closestDiff = previousDiff;
        }

        long nextDiff = Math.abs(nextYearCandidate.toEpochDay() - today.toEpochDay());
        if (nextDiff < closestDiff) {
            closestCandidate = nextYearCandidate;
        }

        return closestCandidate;
    }

    public void setMenu(String menu) {
        for (String m : menu.split("\n")) {
            m = m.trim();
            if (m.isEmpty())
                continue;
            menus.add(m);
        }
    }

    @Override
    public String toString() {
        return String.format("<%s>\n%s\n", date, menus);
    }

    public Menu toMenuResponse() {
        return new Menu(date, menus);
    }
}
