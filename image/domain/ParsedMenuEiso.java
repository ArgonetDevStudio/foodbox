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

        LocalDate today = LocalDate.now(clock);
        YearMonth currentYearMonth = YearMonth.from(today);

        // 현재 년도로 먼저 시도
        LocalDate candidate = LocalDate.of(currentYearMonth.getYear(), month, day);

        // 만약 해당 날짜가 오늘로부터 너무 멀면 다른 년도 시도
        if (Math.abs(candidate.toEpochDay() - today.toEpochDay()) > 45) {
            // 이전 년도 시도
            LocalDate prevYearCandidate = LocalDate.of(currentYearMonth.getYear() - 1, month, day);
            if (Math.abs(prevYearCandidate.toEpochDay() - today.toEpochDay()) < Math.abs(candidate.toEpochDay() - today.toEpochDay())) {
                candidate = prevYearCandidate;
            } else {
                // 다음 년도 시도
                LocalDate nextYearCandidate = LocalDate.of(currentYearMonth.getYear() + 1, month, day);
                if (Math.abs(nextYearCandidate.toEpochDay() - today.toEpochDay()) < Math.abs(candidate.toEpochDay() - today.toEpochDay())) {
                    candidate = nextYearCandidate;
                }
            }
        }

        this.date = candidate;
    }

    private String adjustDateString(String dateStr) {
        return dateStr.replaceAll("윌", "월").trim();
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
