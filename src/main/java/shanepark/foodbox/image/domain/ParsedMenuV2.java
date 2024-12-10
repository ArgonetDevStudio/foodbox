package shanepark.foodbox.image.domain;

import lombok.Getter;
import shanepark.foodbox.api.domain.MenuResponse;

import java.time.LocalDate;
import java.util.ArrayList;
import java.util.List;

@Getter
public class ParsedMenuV2 {
    private final String date;
    private final List<String> menus = new ArrayList<>();

    public ParsedMenuV2(String date) {
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

    public MenuResponse toMenuResponse() {
        LocalDate today = LocalDate.now();
        String[] split = date.split("/");
        int month = Integer.parseInt(split[0]);
        int day = Integer.parseInt(split[1].substring(0, split[1].indexOf("(")));
        LocalDate localDate = LocalDate.of(today.getYear(), month, day);
        if (localDate.isBefore(today.minusMonths(6))) {
            localDate = localDate.plusYears(1);
        }
        return new MenuResponse(localDate, menus);
    }
}
