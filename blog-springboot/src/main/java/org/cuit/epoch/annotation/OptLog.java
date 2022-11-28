package org.cuit.epoch.annotation;

import java.lang.annotation.*;

/**
 * @author: ladidol
 * @date: 2022/11/28 15:00
 * @description: 操作日志注解
 */
@Target(ElementType.METHOD)
@Retention(RetentionPolicy.RUNTIME)
@Documented
public @interface OptLog {

    /**
     * @return 操作类型
     */
    String optType() default "";

}
