package shanepark.foodbox.image.domain;

import lombok.Getter;
import shanepark.foodbox.api.domain.Menu;

import java.time.LocalDate;
import java.util.ArrayList;
import java.util.List;

@Getter
public class ParsedMenu {
    private final String date;
    private final List<String> menus = new ArrayList<>();

    public ParsedMenu(String date) {
        this.date = date.replaceAll("\n", "");
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
        LocalDate today = LocalDate.now();
        String[] split = date.split("/");
        String monthStr = split[0].replaceAll("[^0-9]", "");
        String dayStr = split[1].replaceAll("[^0-9]", "");
        int month = Integer.parseInt(monthStr);
        int day = Integer.parseInt(dayStr);
        LocalDate localDate = LocalDate.of(today.getYear(), month, day);
        if (localDate.isBefore(today.minusMonths(6))) {
            localDate = localDate.plusYears(1);
        }
        return new Menu(localDate, menus);
    }
}
