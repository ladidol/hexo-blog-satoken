package org.cuit.epoch.test;

import cn.dev33.satoken.annotation.*;
import cn.dev33.satoken.stp.StpUtil;
import cn.dev33.satoken.util.SaResult;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

/**
 * 注解鉴权测试 
 * @author kong
 *
 */
@RestController
@RequestMapping("/at/")
public class AtController {

	// 登录认证，登录之后才可以进入方法  ---- http://localhost:8081/at/checkLogin 
	@SaCheckLogin
	@RequestMapping("checkLogin")
	public SaResult checkLogin() {
		return SaResult.ok();
	}
	
	// 权限认证，具备user-add权限才可以进入方法  ---- http://localhost:8081/at/checkPermission 
	@SaCheckPermission("user-add")
	@RequestMapping("checkPermission")
	public SaResult checkPermission() {
		return SaResult.ok();
	}

	// 权限认证，同时具备所有权限才可以进入  ---- http://localhost:8081/at/checkPermissionAnd 
	@SaCheckPermission({"user-add", "user-delete", "user-update"})
	@RequestMapping("checkPermissionAnd")
	public SaResult checkPermissionAnd() {
		return SaResult.ok();
	}

	// 权限认证，只要具备其中一个就可以进入  ---- http://localhost:8081/at/checkPermissionOr 
	@SaCheckPermission(value = {"user-add", "user-delete", "user-update"}, mode = SaMode.OR)
	@RequestMapping("checkPermissionOr")
	public SaResult checkPermissionOr() {
		return SaResult.ok();
	}

	// 角色认证，只有具备admin角色才可以进入  ---- http://localhost:8081/at/checkRole 
	@SaCheckRole("admin")
	@RequestMapping("checkRole")
	public SaResult checkRole() {
		return SaResult.ok();
	}

	// 完成二级认证  ---- http://localhost:8081/at/openSafe 
	@RequestMapping("openSafe")
	public SaResult openSafe() {
		StpUtil.openSafe(200); // 打开二级认证，有效期为200秒
		return SaResult.ok();
	}
	
	// 通过二级认证后才可以进入  ---- http://localhost:8081/at/checkSafe 
	@SaCheckSafe
	@RequestMapping("checkSafe")
	public SaResult checkSafe() {
		return SaResult.ok();
	}
	
	// 通过Basic认证后才可以进入  ---- http://localhost:8081/at/checkBasic 
	@SaCheckBasic(account = "sa:123456")
	@RequestMapping("checkBasic")
	public SaResult checkBasic() {
		return SaResult.ok();
	}
	
}
